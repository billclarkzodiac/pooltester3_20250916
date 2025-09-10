
package main

import (
        "fmt"
        "html/template"
        "math/rand"
        "net/http"
        "time"
)

type DeviceType string

const (
        DevicePump      DeviceType = "Pump"
        DeviceSanitizer DeviceType = "Sanitizer"
        DeviceHeater    DeviceType = "Heater"
        DeviceTruSense  DeviceType = "TruSense"
)

type Device struct {
        ID          string
        Type        DeviceType
        Name        string
        Serial      string
        RPM         int
        RPMPercent  float64
        Temp        float64
        PH          float64
        ORP         float64
}

func makeDemoDevices() []Device {
        var devs []Device
        rand.Seed(time.Now().UnixNano())
        for i := 1; i <= 16; i++ {
                rpm := 1200 + rand.Intn(1800)
                devs = append(devs, Device{
                        ID: fmt.Sprintf("pump-%02d", i),
                        Type: DevicePump,
                        Name: fmt.Sprintf("Pump %02d", i),
                        Serial: fmt.Sprintf("PX%06d", 1000+i),
                        RPM: rpm,
                        RPMPercent: float64(rpm) / 3450.0 * 100,
                        Temp: 24 + rand.Float64()*6,
                })
        }
        for i := 1; i <= 2; i++ {
                devs = append(devs, Device{
                        ID: fmt.Sprintf("heater-%02d", i),
                        Type: DeviceHeater,
                        Name: fmt.Sprintf("Heater %02d", i),
                        Serial: fmt.Sprintf("HX%06d", 2000+i),
                        RPM: 0,
                        RPMPercent: 0,
                        Temp: 30 + rand.Float64()*8,
                })
        }
        for i := 1; i <= 2; i++ {
                devs = append(devs, Device{
                        ID: fmt.Sprintf("sanitizer-%02d", i),
                        Type: DeviceSanitizer,
                        Name: fmt.Sprintf("Sanitizer %02d", i),
                        Serial: fmt.Sprintf("SX%06d", 3000+i),
                        RPM: 0,
                        RPMPercent: 0,
                        Temp: 24 + rand.Float64()*6,
                })
        }
        for i := 1; i <= 2; i++ {
                devs = append(devs, Device{
                        ID: fmt.Sprintf("trusense-%02d", i),
                        Type: DeviceTruSense,
                        Name: fmt.Sprintf("TruSense %02d", i),
                        Serial: fmt.Sprintf("TX%06d", 4000+i),
                        RPM: 0,
                        RPMPercent: 0,
                        Temp: 0,
                        PH: 7.2 + rand.Float64()*0.6,
                        ORP: 600 + rand.Float64()*150,
                })
        }
        return devs
}

func mul(a, b float64) float64 { return a * b }
func div(a, b float64) float64 { return a / b }

var pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{"mul": mul, "div": div}).Parse(`
<html>
<head>
<title>Pool Demo</title>
<meta http-equiv="refresh" content="5">
<style>
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:20px;padding:20px;max-width:1400px}
.card{background:#fff;border-radius:12px;box-shadow:0 4px 6px -1px rgba(0,0,0,0.1);padding:20px;margin-bottom:16px}
.dial{width:70px;height:70px;border-radius:50%;background:#f8fafc;display:flex;align-items:center;justify-content:center;font-size:1.4em;font-weight:bold;margin:10px 0;border:4px solid #2563eb;box-shadow:0 2px 8px rgba(0,0,0,.1)}
.bar{height:10px;border-radius:6px;background:linear-gradient(90deg,#ef4444 0%,#f59e0b 30%,#22c55e 60%,#10b981 100%);margin:8px 0}
.bargray{height:10px;border-radius:6px;background:#e5e7eb;margin:8px 0;position:relative}
.pin{position:absolute;top:-2px;width:4px;height:14px;background:#1f2937;border-radius:2px;transform:translateX(-50%)}
.label{color:#6b7280;font-size:0.85em;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px;font-weight:500}
</style>
</head>
<body style="background:#f6f7fb;font-family:sans-serif">
<h2>Pool Device Demo</h2>
<h3>{{len .}} devices</h3>
<div class="grid">
{{range $index, $device := .}}
<div class="card">
    <div class="label">{{$device.Type}}</div>
    <strong>{{$device.Name}}</strong><br>
    <span style="color:#6b7280">Serial: {{$device.Serial}}</span><br>
    {{if eq $device.Type "Pump"}}
        <div class="dial" style="border-color:#2563eb">{{$device.RPM}}</div>
        <div class="label">RPM</div>
        <div class="bargray" style="margin-bottom:8px;">
            <div class="pin" style="left:{{printf "%.0f" $device.RPMPercent}}%"></div>
        </div>
        <div class="label">Temp: {{printf "%.1f" $device.Temp}}°C</div>
        <div class="bar" style="width:100%"></div>
        <div class="bargray">
            <div class="pin" style="left:{{printf "%.0f" (mul (div $device.Temp 40.0) 100)}}%"></div>
        </div>
    {{else if eq $device.Type "Heater"}}
        <div class="label">Temp: {{printf "%.1f" $device.Temp}}°C</div>
        <div class="bar" style="width:100%"></div>
        <div class="bargray">
            <div class="pin" style="left:{{printf "%.0f" (mul (div $device.Temp 40.0) 100)}}%"></div>
        </div>
    {{else if eq $device.Type "Sanitizer"}}
        <div class="label">Temp: {{printf "%.1f" $device.Temp}}°C</div>
        <div class="bar" style="width:100%"></div>
        <div class="bargray">
            <div class="pin" style="left:{{printf "%.0f" (mul (div $device.Temp 40.0) 100)}}%"></div>
        </div>
    {{else if eq $device.Type "TruSense"}}
        <div class="label">pH: {{printf "%.1f" $device.PH}}</div>
        <div class="bar" style="width:100%"></div>
        <div class="bargray">
            <div class="pin" style="left:{{printf "%.0f" (mul (div $device.PH 8.2) 100)}}%"></div>
        </div>
        <div class="label">ORP: {{printf "%.0f" $device.ORP}} mV</div>
        <div class="bar" style="width:100%"></div>
        <div class="bargray">
            <div class="pin" style="left:{{printf "%.0f" (mul (div $device.ORP 800.0) 100)}}%"></div>
        </div>
    {{end}}
</div>
{{end}}
</div>
</body>
</html>
`))

func main() {
        http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                devices := makeDemoDevices()
                fmt.Printf("Generated %d devices\n", len(devices))
                for i, d := range devices {
                        fmt.Printf("Device %d: %s - %s\n", i, d.Type, d.Name)
                }
                pageTmpl.Execute(w, devices)
        })
        fmt.Println("Demo running at http://localhost:8080")
        http.ListenAndServe(":8080", nil)
}