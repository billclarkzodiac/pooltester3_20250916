
//#define _POSIX_C_SOURCE 

#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/ioctl.h>
#include <sys/time.h>
#include <time.h>
#include <string.h>
#include <errno.h>
#include <netinet/in.h>
#include <linux/if_ether.h>
#include <linux/filter.h>
#include <net/if.h>
#include <arpa/inet.h>
#include <linux/if_packet.h>
#include <net/ethernet.h> /* the L2 protocols */
#include <fcntl.h>
#include <termios.h>
#include <sys/mman.h>
#include <signal.h>

// define this to include a legacy poll message after each set of high speed polls
// #define ALSO_DO_LEGACY_POLL

// high speed baud rate such as B115200
#define HS_BAUDRATE B115200
// how often to send Topology message
#define TOPOLOGY_SECS 4L
// how long to wait after NGA topology message if no answer started
#define IDLE_POLL 150
// how long to wait after NGA topology message if answer has started
#define MAX_RESPONSE_TIME 600

// how many NGA clients we support
#define MAX_CLIENTS 64

// our MQTT port we are listening on
#define OUR_MQTT_PORT 1883

// name of the slip interface
char *SLIP_INTERFACE = "sl0";
// name of the device port for legacy polling
// char *portname = "/dev/ttymxc0";
char *portname = "/dev/ttyUSB0";

// Topology broadcast message - sent from core node, all end devices listen for this
// this is the newer 2025 format containing date/time for end node time sync
typedef struct
{
	// core timestamp seconds since jan 1 2025
	unsigned char secs_lsbyte;
	unsigned char secs_3rd;
	unsigned char secs_2nd;
	unsigned char secs_msbyte;
	// core timestamp fractional seconds, nanoseconds off above
	unsigned char nano_lsbyte;
	unsigned char nano_3rd;
	unsigned char nano_2nd;
	unsigned char nano_msbyte;
	// port number for MQTT connection, 1883 unencrypted, all others encrypted
	unsigned char mqtt_port_lsb;
	unsigned char mqtt_port_msb;
	// number of devices in message
	unsigned char device_count;
	unsigned char core_version; // 0x00 = original topology structure, 0x01 - this one
	// start of devices, possibly none, up to 40
} Topology_Msg_st;
typedef struct
{
	// IP will be 169.254.X.Y where these bytes contain Y and X:
	unsigned char ip_lsb;
	unsigned char ip_msb;
	// duplicate information, if set, IP is detected as a duplication, device should change
	unsigned char duplicate_flag;
	unsigned char serial_number[18];
	unsigned char reserved1;
} Topology_Dev_st;

// IOCTL for custom SLIP kernel module, must match that module code:
#define REG_CURRENT_TASK _IOW('a', 'a', int32_t *)

int IdleTime = 0;
int *Resp;
int *End;
int *Nga;
unsigned char *NewClient;

#if 0
/* CELEBC06

   This example determines the speed of stdout.

 */

char *see_speed(speed_t speed) {
  static char   SPEED[20];
  switch (speed) {
    case B0:       strcpy(SPEED, "B0");
                   break;
    case B50:      strcpy(SPEED, "B50");
                   break;
    case B75:      strcpy(SPEED, "B75");
                   break;
    case B110:     strcpy(SPEED, "B110");
                   break;
    case B134:     strcpy(SPEED, "B134");
                   break;
    case B150:     strcpy(SPEED, "B150");
                   break;
    case B200:     strcpy(SPEED, "B200");
                   break;
    case B300:     strcpy(SPEED, "B300");
                   break;
    case B600:     strcpy(SPEED, "B600");
                   break;
    case B1200:    strcpy(SPEED, "B1200");
                   break;
    case B1800:    strcpy(SPEED, "B1800");
                   break;
    case B2400:    strcpy(SPEED, "B2400");
                   break;
    case B4800:    strcpy(SPEED, "B4800");
                   break;
    case B9600:    strcpy(SPEED, "B9600");
                   break;
    case B19200:   strcpy(SPEED, "B19200");
                   break;
    case B38400:   strcpy(SPEED, "B38400");
                   break;
	case B57600:	strcpy(SPEED, "B57600");
		break;
	case B115200:	strcpy(SPEED, "B115200");
		break;
	case B230400:	strcpy(SPEED, "B230400");
		break;
	case B460800:	strcpy(SPEED, "B460800");
		break;
	case B921600:	strcpy(SPEED, "B921600");
		break;
		
    default:       sprintf(SPEED, "unknown (%d)", (int) speed);
  }
  return SPEED;
}

void print_speed()
{
  struct termios term;
  speed_t speed;

  if (tcgetattr(1, &term) != 0)
    perror("tcgetattr() error");
  else
  {
    speed = cfgetospeed(&term);
    printf("cfgetospeed() says the speed of stdout is %s\n",
           see_speed(speed));
  }
}
#endif


/* msleep(): Sleep for the requested number of milliseconds. */
int msleep(long msec)
{
	struct timespec ts;
	int res;

	if (msec < 0)
	{
		errno = EINVAL;
		return -1;
	}

	ts.tv_sec = msec / 1000;
	ts.tv_nsec = (msec % 1000) * 1000000;

	do
	{
		res = nanosleep(&ts, &ts);
	} while (res && errno == EINTR);

	return res;
}

int filter();
int poll();

int main(int argc, char *argv[])
{
	// shared variables between parent and child process:

	Resp = (int *)mmap(NULL, sizeof(int), PROT_READ | PROT_WRITE,
					   MAP_SHARED | MAP_ANONYMOUS, -1, 0);
	*Resp = 0;

	End = (int *)mmap(NULL, sizeof(int), PROT_READ | PROT_WRITE,
					  MAP_SHARED | MAP_ANONYMOUS, -1, 0);
	*End = 0;

	Nga = (int *)mmap(NULL, sizeof(int), PROT_READ | PROT_WRITE,
					  MAP_SHARED | MAP_ANONYMOUS, -1, 0);
	*Nga = 0;

	NewClient = (unsigned char *)mmap(NULL, 21, PROT_READ | PROT_WRITE,
									  MAP_SHARED | MAP_ANONYMOUS, -1, 0);
	NewClient[0] = 0;

	printf("RS485: using SLIP i/f %s on port = %s\n", SLIP_INTERFACE, portname);
	
	if (fork() == 0)
	{
		// child process, filter actions
		printf("Polling process\n");
		poll();
	}
	else
	{
		// parent, poll
		printf("Filter process\n");
		filter();
	}
}
//
// spawned process that handles Receiving data from end nodes and legacy devices
//
int filter()
{
	int sock; // filter socket
	int n;
	static char buffer[2048];
	struct ifreq ifr;
	struct sockaddr_ll interfaceAddr;
	struct packet_mreq mreq;
	struct sockaddr_ll addr;
	socklen_t addr_len = sizeof(addr);
	int i, sts;

	// open a raw socket
	if ((sock = socket(PF_PACKET, SOCK_RAW, htons(ETH_P_ALL))) < 0) // or maybe ETH_P_IP?
	{
		printf("socker ERR,  error=%d\n", sock);
		perror("socket");
		_exit(1);
	}

	memset(&interfaceAddr, 0, sizeof(interfaceAddr));
	memset(&ifr, 0, sizeof(ifr));
	memset(&mreq, 0, sizeof(mreq));

	memcpy(&ifr.ifr_name, SLIP_INTERFACE, IFNAMSIZ);
	ioctl(sock, SIOCGIFINDEX, &ifr);

	interfaceAddr.sll_ifindex = ifr.ifr_ifindex;
	interfaceAddr.sll_family = AF_PACKET;
	interfaceAddr.sll_protocol = htons(ETH_P_ALL);

	if (bind(sock, (struct sockaddr *)&interfaceAddr, sizeof(interfaceAddr)) < 0)
	{
		printf("bind ERR\n");
		perror("setsockopt bind device");
		close(sock);
		_exit(1);
	}
	// turn on promiscuous flag so we can get data
	struct ifreq ethreq;
	strncpy(ethreq.ifr_name, SLIP_INTERFACE, IF_NAMESIZE);
	if (ioctl(sock, SIOCGIFFLAGS, &ethreq) == -1)
	{
		printf("ioctl1 ERR\n");
		perror("ioctl");
		close(sock);
		_exit(1);
	}
	ethreq.ifr_flags |= IFF_PROMISC;
	if (ioctl(sock, SIOCSIFFLAGS, &ethreq) == -1)
	{
		printf("ioctl2 ERR\n");
		perror("ioctl");
		close(sock);
		_exit(1);
	}

	// bind to slip interface only
	const char *opt;
	opt = SLIP_INTERFACE;
	if (setsockopt(sock, SOL_SOCKET, SO_BINDTODEVICE, opt, strlen(opt) + 1) < 0)
	{
		perror("setsockopt bind device");
		close(sock);
		_exit(1);
	}
	// BPF = Berkley Packet Filter
	// attach the filter to the socket if wanted...since this filter passes all it isn't needed
	// the filter code is generated by running: sudo tcpdump -dd ip
	struct sock_filter BPF_code[] = {
		{0x28, 0, 0, 0x0000000c},
		{0x15, 0, 1, 0x00000800},
		{0x6, 0, 0, 0x00040000},
		{0x6, 0, 0, 0xffffffff}, // 0 drop, -1 keep
	};
	// bpf to only allow IPv4 packets:
	struct sock_filter BPF_code1[] = {
		{0x28, 0, 0, 0x0000000c},
		{0x15, 0, 3, 0x00000800},
		{0x30, 0, 0, 0x00000017},
		{0x15, 0, 1, 0x00000006},
		{0x6, 0, 0, 0x00000000},
		{0x6, 0, 0, 0xffffffff},
		{0x6, 0, 0, 0x00000000},
	};
	struct sock_fprog Filter;
	// error prone code, .len field should be consistent with the real length of the filter code array
	Filter.len = sizeof(BPF_code) / sizeof(BPF_code[0]);
	Filter.filter = BPF_code;

	if (setsockopt(sock, SOL_SOCKET, SO_ATTACH_FILTER, &Filter, sizeof(Filter)) < 0)
	{
		printf("Error attaching filter");
		perror("setsockopt attach filter");
		close(sock);
		_exit(1);
	}

	struct timeval tv;
	tv.tv_sec = 5;
	tv.tv_usec = 1000;
	setsockopt(sock, SOL_SOCKET, SO_RCVTIMEO, (const char *)&tv, sizeof tv);
	//
	// all set, start receiving data
	while (1)
	{
		n = recvfrom(sock, buffer, 2048, 0, (struct sockaddr *)&addr, &addr_len);
		if (n > 0)
		{
			// check for our own transmit data
			if (addr.sll_pkttype == PACKET_OUTGOING)
			{
				printf("OUT:\n");
			}
			else
			{
				*End = 1; // flag end of response
				printf("Slip packet Rx %d bytes\n", (buffer[2] << 8) | buffer[3]);
				IdleTime = 0;
			}
			// look for an Announce message, response to Topology from us
			// user data of 32 = full size of 60 bytes
			if (n == 60)
			{
				if (buffer[28] == 0x55) // should be 0x55
				{
					// client IP broadcast...
					// buffer[29] last byte of IP
					// buffer[30] next to last byte of IP
					// first two bytes are 169.254
					// flag this new guy for polling logic
					if (NewClient[0] == 0)
					{
						NewClient[1] = buffer[30];
						NewClient[2] = buffer[29];
						NewClient[0] = 1;
						memcpy(&NewClient[3], &buffer[32], 18);
						printf("\nAnnounce from %d.%d\n", buffer[30], buffer[29]);
					}
				}
				//	printf("\nFILT(%d): ", n);
				//	for(i = 0; i < n; i++)
				//	{
				//		printf(" %2.2X", buffer[i]);
				//	}
				//	printf("\n");
			}
		}
		else
		{
			IdleTime++;
		}
	}

	return (0);
}

void legacy_poll()
{
	int fd;
	struct termios port;
	speed_t ospeed, ispeed;
	static unsigned char enq[] = {0x10, 0x02, 0x78, 0x00, 0x8A, 0x10, 0x03};
	int i, res;
	int sock_r;
	static struct sockaddr_ll sadr_ll;
	int ifindex;
	int number;

	// slip interface index
	ifindex = if_nametoindex(SLIP_INTERFACE);

	// raw socket
	sock_r = socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL));
	if (sock_r < 0)
	{
		printf("Error on raw sock.");
	}

	sadr_ll.sll_ifindex = ifindex; // index of interface
	sadr_ll.sll_protocol = htons(ETH_P_ALL);
	sadr_ll.sll_family = AF_PACKET;

	fd = open(portname, O_RDWR | O_NOCTTY | O_NONBLOCK ); //O_SYNC);

	if (fd >= 0)
	{
		// stop TTY so no SLIP traffic goes out
		//	number = 100;
		//	ioctl(fd, REG_CURRENT_TASK,(int32_t*) &number);	// tell custom SLIP driver stop tty

		// not exclusive
		ioctl(fd, TIOCEXCL, &res);
		// get current baud rate
		tcgetattr(fd, &port);
		ospeed = cfgetospeed(&port);
		ispeed = cfgetispeed(&port);
		// printf("Pre speeds %u %u\n", ospeed, ispeed);
		//  change to 9600 bps
		cfsetospeed(&port, B9600);
		cfsetispeed(&port, B9600);
		tcsetattr(fd, TCSANOW, &port);
		// transmit
		number = 101;
		ioctl(fd, REG_CURRENT_TASK, (int32_t *)&number); // tell custom SLIP driver start tty

		i = sendto(sock_r, enq, 7, 0, (const struct sockaddr *)&sadr_ll, sizeof(struct sockaddr_ll));

		// re-stop after our transmit, which internally starts TX
		number = 100;
		ioctl(fd, REG_CURRENT_TASK, (int32_t *)&number); // tell custom SLIP driver stop tty
		if (i < 0)
		{
			printf("error in sending....ret=%d....errno=%d\n", i, errno);
		}

		if (i != 7)
		{
			printf("Write %d errno: %d\n", i, errno);
		}
		tcdrain(fd);		   // Wait until transmission ends
		tcflush(fd, TCOFLUSH); // Clear write buffer
		printf("p");
		fflush(stdout);
		// receive
		msleep(100);

		// restore baud rate
		i = cfsetospeed(&port, ospeed);
		// printf("Set ospeed ret %d\n", i);
		i = cfsetispeed(&port, ispeed);
		// printf("Set ispeed ret %d\n", i);
		tcsetattr(fd, TCSANOW, &port);
		ospeed = cfgetospeed(&port);
		ispeed = cfgetispeed(&port);
		// printf("\nPost speeds %u %u\n", ospeed, ispeed);
		close(fd);
	}
	else
	{
		printf("Failed to open serial port %d\n", fd);
	}
	close(sock_r);
}

void sig_event_handler(int n, siginfo_t *info, void *unused)
{
	int check;
	if (n == 44 && *Nga == 1)
	{
		check = info->si_int;
		if (check == 0) //	0xC0 at start of SLIP frame
			*Resp = 1;
		else
			*End = 1; // 0xC0 at end of slip frame

		//        printf ("Signal from kernel : %u\n", check);
	}
}

typedef struct
{
	unsigned char used;
	unsigned char next_to_last;
	unsigned char last;
	unsigned char sn[18];
} ClientIP_st;

ClientIP_st Clients[MAX_CLIENTS];

int poll()
{
	int bcast_sock;
#define BROADCAST_PORT 30000u
	struct sockaddr_in s;
	char mess[8];
	int i, sts;
	int idle;
	int fd;
	struct termios port;
	struct sigaction act;
	int number;
	int existing;
	unsigned long last_topology = 0L;
	unsigned long now;
	Topology_Msg_st *topo;
	unsigned char DeviceCount = 0;
	int xmit_len = 0;
	Topology_Dev_st *dev;
	int used;
	struct timeval tv;
	unsigned int m;

	/* install custom signal handler */
	sigemptyset(&act.sa_mask);
	act.sa_flags = (SA_SIGINFO | SA_RESTART);
	act.sa_sigaction = sig_event_handler;
	// sigaction(44, &act, NULL);

	// change SLIP baud rate to desired baud rate
	fd = open(portname, O_RDWR | O_NOCTTY | O_SYNC);
	number = 0;
	ioctl(fd, REG_CURRENT_TASK, (int32_t *)&number); // tell driver we are the task to signal
	tcgetattr(fd, &port);
	cfsetospeed(&port, HS_BAUDRATE);
	cfsetispeed(&port, HS_BAUDRATE);
	tcsetattr(fd, TCSANOW, &port);

	bcast_sock = socket(AF_INET, SOCK_DGRAM, 0);
	int broadcastEnable = 1;
	int ret = setsockopt(bcast_sock, SOL_SOCKET, SO_BROADCAST, &broadcastEnable, sizeof(broadcastEnable));

	memset(&s, '\0', sizeof(struct sockaddr_in));
	s.sin_family = AF_INET;
	s.sin_port = htons(BROADCAST_PORT);
	s.sin_addr.s_addr = inet_addr("169.254.255.255");

	ioctl(fd, REG_CURRENT_TASK, (int32_t *)&number); // tell driver start tty

	topo = calloc(1, sizeof(Topology_Msg_st) + sizeof(Topology_Dev_st) * MAX_CLIENTS);

	while (1)
	{
#ifdef ALSO_DO_LEGACY_POLL
		// start TTY
		number = 101;
		ioctl(fd, REG_CURRENT_TASK, (int32_t *)&number); // tell custom SLIP driver start tty
#endif

		// see if any new clients available
		if (NewClient[0] != 0)
		{
			for (existing = 0, i = 0; i < MAX_CLIENTS; i++)
			{
				if (Clients[i].used != 0)
				{
					if (Clients[i].next_to_last == NewClient[1] && Clients[i].last == NewClient[2])
					{
						existing = 1;
						break;
					}
				}
			}
			if (existing == 0)
			{
				for (i = 0; i < MAX_CLIENTS; i++)
				{
					if (Clients[i].used == 0)
					{
						Clients[i].next_to_last = NewClient[1];
						Clients[i].last = NewClient[2];
						memcpy(Clients[i].sn, &NewClient[3], 18);
						Clients[i].used = 1;
						printf("\nNEW Topology Entry %d %d\n", NewClient[1], NewClient[2]);
						DeviceCount++;
						break;
					}
				}
			}

			// clear flag
			NewClient[0] = 0;
		}

		// see if time to broadcast Topology message again...
		now = time(0L);

		if (now - last_topology > TOPOLOGY_SECS)
		{
			last_topology = now;
			xmit_len = sizeof(Topology_Msg_st);
			// point to first area for devices
			dev = (Topology_Dev_st *)(&topo->core_version + 1);
			// fill in topology message with known clients
			topo->core_version = 1;

			topo->mqtt_port_lsb = OUR_MQTT_PORT & 0x00ff;
			topo->mqtt_port_msb = (OUR_MQTT_PORT >> 8) & 0x00ff;

			topo->device_count = DeviceCount;

			// get current time on this unit
			gettimeofday(&tv, NULL);

			topo->secs_lsbyte = tv.tv_sec & 0x00ff;
			topo->secs_3rd = (tv.tv_sec >> 8) & 0x00ff;
			topo->secs_2nd = (tv.tv_sec >> 16) & 0x00ff;
			topo->secs_msbyte = (tv.tv_sec >> 24) & 0x00ff;

			m = tv.tv_usec * 1000;
			topo->nano_lsbyte = m & 0x00ff;
			topo->nano_3rd = (m >> 8) & 0x00ff;
			topo->nano_2nd = (m >> 16) & 0x00ff;
			topo->nano_msbyte = (m >> 24) & 0x00ff;

			for (i = 0, used = 0; i < MAX_CLIENTS; i++)
			{
				if (Clients[i].used != 0)
				{
					// increase number of bytes in our broadcast message
					xmit_len += sizeof(Topology_Dev_st);

					// fill in device data for this client
					dev->ip_lsb = Clients[i].last;
					dev->ip_msb = Clients[i].next_to_last;
					dev->duplicate_flag = 0;
					memcpy(dev->serial_number, Clients[i].sn, 18);

					dev++; // point to next device area in message
					used++;
				}
			}
			sts = sendto(bcast_sock, topo, xmit_len, 0, (struct sockaddr *)&s, sizeof(struct sockaddr_in));
//			if(0 != sts) printf("line 563: sts = %d\n", sts);
			tcdrain(fd);		   // Wait until transmission ends
			tcflush(fd, TCOFLUSH); // Clear write buffer
			printf("TOPO msg of %d bytes for %d clients\n", xmit_len, DeviceCount);
			fflush(stdout);
			msleep(500);
		}

		// Poll the NGA clients we know about for data, their responses will
		// go into the TCP/IP stack
		for (i = 0; i < MAX_CLIENTS; i++)
		{
			if (Clients[i].used != 0)
			{
				mess[0] = 169;
				mess[1] = 254;
				mess[2] = Clients[i].next_to_last;
				mess[3] = Clients[i].last;
				*Resp = 0;
				*End = 0;
				*Nga = 1;
				sts = sendto(bcast_sock, mess, 4, 0, (struct sockaddr *)&s, sizeof(struct sockaddr_in));
//				if(0 != sts) printf("line 585: sts = %d\n", sts);
				tcdrain(fd);		   // Wait until transmission ends
				tcflush(fd, TCOFLUSH); // Clear write buffer
				printf("%d, ", i + 1);
				fflush(stdout);
				idle = 0;
				while (*Resp == 0 ? idle < IDLE_POLL : idle < MAX_RESPONSE_TIME)
				{
					msleep(1);
					idle++;
					if (*End != 0 && *Resp != 0)
						break;
				}
				if (*Resp == 1)
					printf("X"), fflush(stdout);
			}
		}

		*Nga = 0;
// Now do a legacy poll
#ifdef ALSO_DO_LEGACY_POLL
		legacy_poll();
#endif
	}

	return (0);
}
