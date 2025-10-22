
```text
███╗   ██╗ ██████╗  █████╗ ███████╗██╗███╗   ███╗  
████╗  ██║██╔════╝ ██╔══██╗██╔════╝██║████╗ ████║  
██╔██╗ ██║██║  ███╗███████║███████╗██║██╔████╔██║  
██║╚██╗██║██║   ██║██╔══██║╚════██║██║██║╚██╔╝██║  
██║ ╚████║╚██████╔╝██║  ██║███████║██║██║ ╚═╝ ██║  
╚═╝  ╚═══╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝╚═╝     ╚═╝  
```  

# 🏊‍♂️ Pool Controller & Device Discovery System

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go)](https://golang.org/)
[![MQTT](https://img.shields.io/badge/MQTT-Protocol-purple?style=for-the-badge)](https://mqtt.org/)
[![Protobuf](https://img.shields.io/badge/Protocol-Buffers-blue?style=for-the-badge)](https://developers.google.com/protocol-buffers)

**Enterprise-grade pool automation with automatic device discovery**

[🚀 Quick Start](#-quick-start) • 
[📚 Documentation](#-documentation) • 
[🧪 Testing](#-testing) • 
[🏗️ Architecture](#-architecture)

---

## ✨ **What is NgaSim?**

NgaSim is a multi-device pool controller that automatically discovers and manages pool equipment using MQTT communication and Protocol Buffers. Built for reliability, extensibility, and easy maintenance.

### **🎯 Key Features**

- **🔍 Automatic Discovery** - Finds pool devices without configuration
- **🧬 Protocol Buffers** - Efficient, typed messaging between devices  
- **📡 MQTT Communication** - Industrial IoT messaging protocol
- **🌐 Web Dashboard** - Real-time device monitoring and control
- **⚡ Multi-Device Support** - Sanitizers, controllers, pumps, boosters
- **🧪 Comprehensive Testing** - Automated validation and monitoring

## 🚀 **Quick Start**

```bash
# Clone and build
git clone https://github.com/your-org/ngasim-pool-controller
cd ngasim-pool-controller
go build -o pool-controller

# Run tests
./continuous_test.sh

# Start system  
./pool-controller &

# View dashboard
curl http://localhost:8082
```
📊 Supported Devices
Device Type	Status	Features
🧪 Sanitizer Gen2	✅ Production	Chemical monitoring, automated dosing
🎛️ Digital Controller	✅ Production	Temperature, flow control, automation
⚡ SpeedSet Plus Pumps	✅ Production	Variable speed, energy management
🚀 VSP Booster	🔄 Ready to add	Booster pump control

**🏗️ Architecture**
```text
┌─────────────────┐    MQTT/Protobuf    ┌─────────────────┐    HTTP/JSON    ┌─────────────────┐  
│   Pool Device   │ ◄─────────────────► │  NgaSim Core    │ ◄─────────────► │  Web Dashboard  │  
│                 │                     │                 │                 │                 │  
│ • Sanitizer     │   Device Discovery  │ • Auto Discovery│   Real-time     │ • Device Status │  
│ • Controller    │   Status Updates    │ • Device Mgmt   │   Updates       │ • Control Panel │  
│ • Pump          │   Commands          │ • Web Server    │   Device Ctrl   │ • Monitoring    │  
└─────────────────┘                     └─────────────────┘                 └─────────────────┘  
```

**Documentation**

- **Testing Guide** - Comprehensive testing procedures
- **Development Status** - Project history and decisions
- **Architecture Overview** - System design and patterns
- **API Reference** - Web interface and device communication

**🧪 Testing**  
```sh
# Automated testing suite
./continuous_test.sh

# Real-time monitoring
./monitor_devices.sh &

# Load testing
./load_test.sh

# Manual device testing
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST001/anc" -m "test"
```
**Contributing**
1. **Fork** the repository  
2. **Create** feature branch (git checkout -b feature/amazing-feature)  
3. **Test** thoroughly (continuous_test.sh)  
4. **Commit** changes (git commit -m 'Add amazing feature')  
5. **Push** to branch (git push origin feature/amazing-feature)  
6. **Create** Pull Request  

**📄 License**

This project is licensed under the MIT License - see the LICENSE file for details.

**🙏 Acknowledgments**
- Built for enterprise pool automation
- Designed for easy handoff and maintenance
- Comprehensive testing and documentation
- Production-ready architect
---