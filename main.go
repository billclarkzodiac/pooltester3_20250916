package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
//	"NgaSim/ned"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Event struct {
	TS   time.Time `json:"ts"`
	Kind string    `json:"kind"`
	Tag  string    `json:"tag,omitempty"`
}

type Device struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Name          string            `json:"name"`
	Serial        string            `json:"serial"`
	Mode          string            `json:"mode"`
	RPM           int               `json:"rpm"`
	PowerWatts    int               `json:"power_watts"`
	TempC         float64           `json:"temp_c"`
	PH            float64           `json:"ph"`
	ORP           float64           `json:"orp"`
	LastAnnounce  time.Time         `json:"last_announce"`
	LastTelemetry time.Time         `json:"last_telemetry"`
	Meta          map[string]string `json:"meta,omitempty"`
}

type sseClient struct{ ch chan []byte }

type Hub struct {
	mu      sync.RWMutex
	devices map[string]*Device
	clients map[*sseClient]struct{}
	mqttc   mqtt.Client
}

func NewHub() *Hub { return &Hub{devices: map[string]*Device{}, clients: map[*sseClient]struct{}{}} }

func (h *Hub) addClient(c *sseClient)   { h.mu.Lock(); h.clients[c] = struct{}{}; h.mu.Unlock() }
func (h *Hub) removeClient(c *sseClient) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }

func (h *Hub) snapshot() []*Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Device, 0, len(h.devices))
	for _, d := range h.devices {
		out = append(out, d)
	}
	return out
}

func (h *Hub) broadcast(kind string, payload any) {
	msg := struct {
		Kind string `json:"kind"`
		Data any    `json:"data"`
	}{Kind: kind, Data: payload}
	b, _ := json.Marshal(msg)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.ch <- b:
		default:
		}
	}
}

func (h *Hub) upsertDevice(d *Device) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.devices[d.ID]; ok {
		if d.Type != "" {
			cur.Type = d.Type
		}
		if d.Name != "" {
			cur.Name = d.Name
		}
		if d.Serial != "" {
			cur.Serial = d.Serial
		}
		if d.Mode != "" {
			cur.Mode = d.Mode
		}
		if d.RPM != 0 {
			cur.RPM = d.RPM
		}
		if d.PowerWatts != 0 {
			cur.PowerWatts = d.PowerWatts
		}
		if d.TempC != 0 {
			cur.TempC = d.TempC
		}
		if d.PH != 0 {
			cur.PH = d.PH
		}
		if d.ORP != 0 {
			cur.ORP = d.ORP
		}
		if !d.LastAnnounce.IsZero() {
			cur.LastAnnounce = d.LastAnnounce
		}
		if !d.LastTelemetry.IsZero() {
			cur.LastTelemetry = d.LastTelemetry
		}
		if d.Meta != nil {
			if cur.Meta == nil {
				cur.Meta = map[string]string{}
			}
			for k, v := range d.Meta {
				cur.Meta[k] = v
			}
		}
		return
	}
	h.devices[d.ID] = d
}

// --- MQTT wiring ---

var reAnn = regexp.MustCompile(`^devices/([^/]+)/announce$`)
var reTel = regexp.MustCompile(`^devices/([^/]+)/telemetry$`)

func (h *Hub) startMQTT(broker string) {
	opts := mqtt.NewClientOptions().AddBroker(broker)
	opts.SetClientID(fmt.Sprintf("tester-%d", time.Now().UnixNano()))
	opts.SetOrderMatters(false)
	h.mqttc = mqtt.NewClient(opts)
	if tok := h.mqttc.Connect(); tok.Wait() && tok.Error() != nil {
		log.Fatalf("mqtt connect: %v", tok.Error())
	}

	if tok := h.mqttc.Subscribe("devices/+/announce", 0, func(_ mqtt.Client, m mqtt.Message) {
		id := ""
		if sm := reAnn.FindStringSubmatch(m.Topic()); len(sm) == 2 {
			id = sm[1]
		}
		var d Device
		if err := json.Unmarshal(m.Payload(), &d); err == nil {
			if d.ID == "" {
				d.ID = id
			}
			d.LastAnnounce = time.Now()
			h.upsertDevice(&d)
			h.broadcast("announce", d)
		}
	}); tok.Wait() && tok.Error() != nil {
		log.Fatalf("subscribe announce: %v", tok.Error())
	}

	if tok := h.mqttc.Subscribe("devices/+/telemetry", 0, func(_ mqtt.Client, m mqtt.Message) {
		id := ""
		if sm := reTel.FindStringSubmatch(m.Topic()); len(sm) == 2 {
			id = sm[1]
		}
		var td map[string]any
		if err := json.Unmarshal(m.Payload(), &td); err == nil {
			d := &Device{ID: id, LastTelemetry: time.Now()}
			if v, ok := td["type"].(string); ok {
				d.Type = v
			}
			if v, ok := td["name"].(string); ok {
				d.Name = v
			}
			if v, ok := td["serial"].(string); ok {
				d.Serial = v
			}
			if v, ok := td["mode"].(string); ok {
				d.Mode = v
			}
			if v, ok := td["rpm"].(float64); ok {
				d.RPM = int(v)
			}
			if v, ok := td["power_watts"].(float64); ok {
				d.PowerWatts = int(v)
			}
			if v, ok := td["temp_c"].(float64); ok {
				d.TempC = v
			}
			if v, ok := td["ph"].(float64); ok {
				d.PH = v
			}
			if v, ok := td["orp"].(float64); ok {
				d.ORP = v
			}
			h.upsertDevice(d)
			h.broadcast("telemetry", struct {
				ID string         `json:"id"`
				TD map[string]any `json:"td"`
			}{ID: id, TD: td})
		}
	}); tok.Wait() && tok.Error() != nil {
		log.Fatalf("subscribe telemetry: %v", tok.Error())
	}
}

func (h *Hub) publishBridgeEvent(e Event) {
	// Publish to MQTT (optional) and always broadcast to UI
	if h.mqttc != nil && h.mqttc.IsConnectionOpen() {
		e.TS = time.Now()
		b, _ := json.Marshal(e)
		ok := h.mqttc.Publish("tester/poller/events", 0, false, b)
		ok.Wait()
	}
	h.broadcast("poller", e)
}

// --- Poller child + stdout parser ---

func runPollerBridge(ctx context.Context, hub *Hub, pollerPath string) {
	cmd := exec.CommandContext(ctx, pollerPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("start poller: %v", err)
	}
	log.Printf("started poller pid=%d", cmd.Process.Pid)

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Printf("[poller] %s", sc.Text())
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Split(bufio.ScanRunes)
	for sc.Scan() {
		ch := strings.TrimSpace(sc.Text())
		if ch == "" {
			continue
		}
		switch ch {
		case "1", "2", "3", "4":
			hub.publishBridgeEvent(Event{Kind: "probe-start", Tag: ch})
		case "X":
			hub.publishBridgeEvent(Event{Kind: "resp"})
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("poller scanner: %v", err)
	}
	_ = cmd.Wait()
	log.Printf("poller exited")
}

// --- Demo data: 16 pumps, 2 heaters, 2 sanitizers with pH/ORP ---

func startDemoData(h *Hub, pumps, heaters, sanitizers int) {
	rand.Seed(time.Now().UnixNano())
	now := time.Now()
	announce := func(d *Device) {
		d.LastAnnounce = now
		d.LastTelemetry = now
		h.upsertDevice(d)
		h.broadcast("announce", *d)
	}

	for i := 1; i <= pumps; i++ {
		id := fmt.Sprintf("pump-%02d", i)
		announce(&Device{ID: id, Type: "pump", Name: fmt.Sprintf("Pump %02d", i), Serial: fmt.Sprintf("PX%06d", 1000+i), Mode: "Normal", RPM: 1200 + rand.Intn(1800), PowerWatts: 250 + rand.Intn(900), TempC: 24 + rand.Float64()*6})
	}
	for i := 1; i <= heaters; i++ {
		id := fmt.Sprintf("heater-%02d", i)
		modes := []string{"Idle", "Heating"}
		announce(&Device{ID: id, Type: "heater", Name: fmt.Sprintf("Heater %02d", i), Serial: fmt.Sprintf("HX%06d", 2000+i), Mode: modes[rand.Intn(2)], PowerWatts: 800 + rand.Intn(800), TempC: 30 + rand.Float64()*8})
	}
	for i := 1; i <= sanitizers; i++ {
		id := fmt.Sprintf("san-%02d", i)
		announce(&Device{ID: id, Type: "sanitizer", Name: fmt.Sprintf("Sanitizer %02d", i), Serial: fmt.Sprintf("SX%06d", 3000+i), Mode: "Normal", PowerWatts: 50 + rand.Intn(80), TempC: 24 + rand.Float64()*6, PH: 7.4, ORP: 650})
	}

	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for range t.C {
			snap := h.snapshot()
			for _, cur := range snap {
				d := &Device{ID: cur.ID, LastTelemetry: time.Now()}
				switch cur.Type {
				case "pump":
					rpm := cur.RPM + rand.Intn(121) - 60
					if rpm < 0 {
						rpm = 0
					}
					if rpm > 3450 {
						rpm = 3450
					}
					d.RPM = rpm
					d.PowerWatts = int(0.0001*float64(rpm*rpm)) + 150 + rand.Intn(50)
					if rand.Intn(10) == 0 {
						d.Mode = "Priming"
					} else {
						d.Mode = "Normal"
					}
					d.TempC = cur.TempC + (rand.Float64()*0.2 - 0.1)
				case "heater":
					target := 32.0
					if cur.Mode == "Heating" {
						target = 38.0
					}
					d.TempC = cur.TempC + (target-cur.TempC)*0.05 + (rand.Float64()*0.2 - 0.1)
					d.PowerWatts = 700 + rand.Intn(900)
				case "sanitizer":
					d.PowerWatts = 40 + rand.Intn(60)
					d.TempC = cur.TempC + (rand.Float64()*0.2 - 0.1)
					ph := cur.PH + (rand.Float64()*0.06 - 0.03)
					if ph < 7.2 {
						ph = 7.2
					}
					if ph > 7.8 {
						ph = 7.8
					}
					d.PH = ph
					orp := cur.ORP + float64(rand.Intn(21)-10)
					if orp < 600 {
						orp = 600
					}
					if orp > 750 {
						orp = 750
					}
					d.ORP = orp
				}
				h.upsertDevice(d)
				payload := map[string]any{"mode": d.Mode, "rpm": d.RPM, "power_watts": d.PowerWatts, "temp_c": d.TempC, "ph": d.PH, "orp": d.ORP}
				h.broadcast("telemetry", struct {
					ID string         `json:"id"`
					TD map[string]any `json:"td"`
				}{ID: d.ID, TD: payload})
			}
		}
	}()
}

// --- HTTP: page + SSE with Home and Pumps tabs + pH/ORP bars ---

const pageHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Pool Tester</title>
<style>
:root{--card:#fff;--bg:#f6f7fb;--ink:#111;--muted:#6b7280}
html,body{margin:0;height:100%;background:var(--bg);color:var(--ink);font:14px system-ui,Segoe UI,Roboto,Helvetica,Arial}
header{position:sticky;top:0;background:#fff;box-shadow:0 1px 8px rgba(0,0,0,.06);padding:12px 16px;display:flex;gap:16px;align-items:center}
nav a{margin-right:12px;text-decoration:none;color:var(--muted)}
nav a.active{color:var(--ink);font-weight:600}
main{padding:16px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:16px}
.card{background:#fff;border-radius:16px;box-shadow:0 2px 12px rgba(0,0,0,.06);padding:16px}
.row{display:flex;align-items:center;justify-content:space-between;gap:12px}
.kv{display:grid;grid-template-columns:110px 1fr;gap:6px 12px;margin-top:8px}
.kv div:nth-child(odd){color:var(--muted)}
.bar{height:10px;border-radius:999px;background:linear-gradient(90deg,#2563eb,#22c55e,#fde047,#f97316,#ef4444)}
.bargray{height:10px;border-radius:999px;background:#e5e7eb;margin-top:6px;position:relative}
.pin{position:absolute;top:-3px;width:2px;height:16px;background:#111}
#events{max-height:160px;overflow:auto;background:#fff;border-radius:12px;padding:8px;box-shadow:0 2px 12px rgba(0,0,0,.06)}
</style>
</head>
<body>
<header>
  <strong>Pool Tester</strong>
  <nav>
    <a href="#" id="nav-home" class="active">Home</a>
    <a href="#pumps" id="nav-pumps">Pumps</a>
  </nav>
  <span id="status" style="color:var(--muted)">connecting…</span>
</header>
<main>
  <div class="row">
    <div style="flex:1">
      <h3 style="margin:8px 0">Devices</h3>
      <div id="devices" class="grid"></div>
    </div>
    <div style="width:360px">
      <h3 style="margin:8px 0">Live events</h3>
      <div id="events"></div>
    </div>
  </div>
</main>
<script>
const $ = s=>document.querySelector(s);
let route = location.hash||'#';
function setRoute(r){ route=r; document.querySelectorAll('nav a').forEach(a=>a.classList.remove('active')); if(route==='#pumps') $('#nav-pumps').classList.add('active'); else $('#nav-home').classList.add('active'); render(window.__DEVICES__||[]); }
window.addEventListener('hashchange', ()=> setRoute(location.hash||'#'));

function fmt(t){ try{ return new Date(t).toLocaleTimeString(); }catch(e){ return t; } }
function clamp(v,a,b){ return Math.max(a, Math.min(b, v)); }

function fmtNum(val){
  if(val===undefined||val===null||Number.isNaN(val)) return "-";
  if(typeof val==='number') return val.toFixed(2);
  return val;
}

function phPct(ph){ return clamp(((ph-6.8)/(8.2-6.8))*100, 0, 100); }
function orpPct(orp){ return clamp(((orp-500)/(800-500))*100, 0, 100); }

function phBar(ph){
  const pct = phPct(ph);
  return '<div class="bar"></div><div class="bargray"><div class="pin" style="left:'+pct+'%"></div></div>';
}
function orpBar(orp){
  const pct = orpPct(orp);
  return '<div class="bar"></div><div class="bargray"><div class="pin" style="left:'+pct+'%"></div></div>';
}

function render(devs){
  const root = document.getElementById("devices");
  root.innerHTML = "";
  // filter for route
  const list = (route==='#pumps') ? devs.filter(d=> (d.type||'').toLowerCase()==='pump') : devs.slice();
  // sort ascending by name (fallback to id)
  list.sort((a,b)=>{
    const ka = (a.name||a.id||'').toLowerCase();
    const kb = (b.name||b.id||'').toLowerCase();
    if(ka<kb) return -1; if(ka>kb) return 1; return 0;
  });

  list.forEach(d=>{
    const el = document.createElement('div'); el.className='card';
    let phHTML = '', orpHTML='';
    if(typeof d.ph === 'number') { phHTML = '<div>pH</div><div>'+fmtNum(d.ph)+phBar(d.ph)+'</div>'; }
    if(typeof d.orp === 'number') { orpHTML = '<div>ORP</div><div>'+Math.round(d.orp)+' mV'+orpBar(d.orp)+'</div>'; } // integer ORP
    el.innerHTML =
      '<div class="row"><strong>'+(d.name||d.id)+'</strong><span style="color:#6b7280">'+(d.type||'device')+'</span></div>'+
      '<div class="kv">'+
      '<div>Serial</div><div>'+(d.serial||'-')+'</div>'+
      '<div>Mode</div><div>'+(d.mode||'-')+'</div>'+
      '<div>RPM</div><div>'+(d.rpm||0)+'</div>'+
      '<div>Power</div><div>'+(d.power_watts||0)+' W</div>'+
      '<div>Temp</div><div>'+fmtNum(d.temp_c)+' °C</div>'+
      phHTML+orpHTML+
      '<div>Announce</div><div>'+(d.last_announce?fmt(d.last_announce):'-')+'</div>'+
      '<div>Telemetry</div><div>'+(d.last_telemetry?fmt(d.last_telemetry):'-')+'</div>'+
      '</div>';
    root.appendChild(el);
  });
}

async function loadSnapshot(){ try{ const r = await fetch('/api/snapshot'); const devs = await r.json(); window.__DEVICES__ = devs; render(devs);}catch(e){} }

function startSSE(){
  const st = document.getElementById('status');
  const es = new EventSource('/events');
  es.onopen = ()=> st.textContent = 'live';
  es.onerror = ()=> st.textContent = 'disconnected';
  es.onmessage = (ev)=>{
    try{ const msg = JSON.parse(ev.data);
      if(msg.kind==='snapshot'){ window.__DEVICES__ = msg.data||[]; render(window.__DEVICES__); }
      else if(msg.kind==='announce'){ loadSnapshot(); }
      else if(msg.kind==='telemetry'){ loadSnapshot(); }
      else if(msg.kind==='poller'){ const e = msg.data; const box = document.getElementById('events'); const line = document.createElement('div'); line.textContent = (e.kind==='probe-start'?('probe '+(e.tag||'')):'response'); box.prepend(line); }
    }catch(e){}
  };
}

setRoute(route);
loadSnapshot();
startSSE();
</script>
</body>
</html>`

// --- HTTP SSE handler ---

func sseHandler(h *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		c := &sseClient{ch: make(chan []byte, 64)}
		h.addClient(c)
		defer h.removeClient(c)

		init := struct {
			Kind string    `json:"kind"`
			Data []*Device `json:"data"`
		}{"snapshot", h.snapshot()}
		b, _ := json.Marshal(init)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case msg := <-c.ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			}
		}
	}
}

func main() {
	pollerPath := flag.String("poller", "/bin/true", "path to C poller binary")
	mqttURL := flag.String("mqtt", "tcp://localhost:1883", "MQTT broker URL")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	demo := flag.Bool("demo", true, "generate fake devices/telemetry in-process")
	flag.Parse()

	hub := NewHub()
	hub.startMQTT(*mqttURL)
	if *demo {
		go startDemoData(hub, 16, 2, 2)
	}
	go runPollerBridge(context.Background(), hub, *pollerPath)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(pageHTML))
	})
	http.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hub.snapshot())
	})
	http.HandleFunc("/events", sseHandler(hub))

	log.Printf("Serving UI at http://localhost%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
