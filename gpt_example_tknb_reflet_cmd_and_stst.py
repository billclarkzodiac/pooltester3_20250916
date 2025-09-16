import os
import sys
import importlib
import tkinter as tk
from tkinter import ttk, messagebox
import threading
import time
import uuid
from datetime import datetime
import paho.mqtt.client as mqtt

import sanitizer_pb2
import get_device_information_pb2
import get_device_configuration_pb2
import commonClientMessages_pb2

# Import DCT/ICL protobuf
try:
    import digitalControllerTransformer_pb2
    HAS_DCT = True
except ImportError:
    HAS_DCT = False

BROKER_ADDRESS = "localhost"
PORT = 1883

active_devices = {}
latest_announcement = {}
latest_info = {}
latest_telemetry = {}
device_config = {}
tab_widgets = {}

refresh_event = threading.Event()
active_lock = threading.Lock()
announce_lock = threading.Lock()
info_lock = threading.Lock()
telemetry_lock = threading.Lock()

client = mqtt.Client(callback_api_version=mqtt.CallbackAPIVersion.VERSION2)

def build_topic(serial, category, kind):
    if kind == "cmd":
        return f"cmd/{category}/{serial}/req"
    return f"async/{category}/{serial}/{kind}"

def on_connect(client, userdata, flags, rc, properties=None):
    if rc == 0:
        client.subscribe("async/+/+/anc")
        client.subscribe("async/+/+/info")
        client.subscribe("async/+/+/dt")
        client.subscribe("async/+/+/error")
    else:
        print(f"Failed to connect: {rc}")

def reflect_message(msg, indent=0):
    """Recursively display all fields (including nested) of a protobuf message."""
    lines = []
    for field, value in msg.ListFields():
        name = field.name
        if field.type == field.TYPE_MESSAGE:
            lines.append(" " * indent + f"{name}:")
            if field.label == field.LABEL_REPEATED:
                for item in value:
                    lines.append(reflect_message(item, indent + 2))
            else:
                lines.append(reflect_message(value, indent + 2))
        else:
            lines.append(" " * indent + f"{name}: {value}")
    return "\n".join(lines)

def log_status(tab_widgets, serial, message, color="black"):
    log_text = tab_widgets[serial]['log_text']
    log_text.config(state="normal")
    log_text.insert(tk.END, f"{datetime.now().strftime('%H:%M:%S')} {message}\n")
    log_text.tag_add(color, "end-2l", "end-1l")
    log_text.tag_config(color, foreground=color)
    log_text.config(state="disabled")
    log_text.see(tk.END)

def get_device_type(serial):
    """Return 'icl' for DCT/ICL, 'sanitizer' for sanitizer, or None if unknown."""
    category = active_devices.get(serial, {}).get("category", "").lower()
    if HAS_DCT and ("dct" in category or "digitalcontroller" in category or "icl" in category or "infinite color" in category):
        return "icl"
    elif "sanitizer" in category:
        return "sanitizer"
    return None

def on_message(client, userdata, message):
    try:
        parts = message.topic.split('/')
        if len(parts) < 4:
            return
        category, serial, msg_type = parts[1], parts[2], parts[3]

        dev_type = get_device_type(serial)

        if msg_type == "anc":
            ann = get_device_information_pb2.GetDeviceInformationResponsePayload()
            ann.ParseFromString(message.payload)
            with announce_lock:
                active_devices[ann.serial_number] = {
                    "category": ann.category,
                    "product_name": ann.product_name
                }
                if ann.serial_number not in device_config:
                    device_config[ann.serial_number] = {
                        'level': 5,
                        'interval': 4,
                        'sending': False,
                        'thread': None,
                        'stop_event': threading.Event()
                    }
                latest_announcement[ann.serial_number] = ann
                refresh_event.set()
            if ann.serial_number in tab_widgets and 'log_text' in tab_widgets[ann.serial_number]:
                log_status(tab_widgets, ann.serial_number, "--- Announcement ---\n" + reflect_message(ann), "blue")
        elif msg_type == "info":
            info = get_device_configuration_pb2.GetDeviceConfigurationResponsePayload()
            info.ParseFromString(message.payload)
            with info_lock:
                latest_info[serial] = info
                refresh_event.set()
            if serial in tab_widgets and 'log_text' in tab_widgets[serial]:
                log_status(tab_widgets, serial, "--- Info ---\n" + reflect_message(info), "blue")
        elif msg_type == "dt":
            # Separate telemetry parsing by device type
            if dev_type == "icl":
                telemetry = digitalControllerTransformer_pb2.TelemetryMessage()
            elif dev_type == "sanitizer":
                telemetry = sanitizer_pb2.TelemetryMessage()
            else:
                # Unknown device type, skip
                return
            telemetry.ParseFromString(message.payload)
            with telemetry_lock:
                latest_telemetry[serial] = telemetry
                refresh_event.set()
            if serial in tab_widgets and 'log_text' in tab_widgets[serial]:
                log_status(tab_widgets, serial, "--- Telemetry ---\n" + reflect_message(telemetry), "green")
        elif msg_type == "cmdr" and dev_type == "sanitizer":
            # Handle sanitizer CommandResponseMessage
            try:
                resp = sanitizer_pb2.CommandResponseMessage()
                resp.ParseFromString(message.payload)
                # Build a readable string using protobuf labels and values
                def format_field(field, value, indent=0):
                    label = field.name.replace("_", " ").title()
                    if field.type == field.TYPE_MESSAGE:
                        if field.label == field.LABEL_REPEATED:
                            return "\n".join(
                                " " * indent + f"{label}:\n" + format_field(field.message_type.fields[0], v, indent + 2)
                                for v in value
                            )
                        else:
                            return " " * indent + f"{label}:\n" + reflect_message(value, indent + 2)
                    else:
                        return " " * indent + f"{label}: {value}"

                lines = []
                for field, value in resp.ListFields():
                    lines.append(format_field(field, value))
                if lines:
                    msg = "--- Sanitizer Command Response ---\n" + "\n".join(lines)
                else:
                    msg = "--- Sanitizer Command Response ---\n(No fields in response)"
            except Exception:
                # If parsing fails, show raw hex
                msg = "--- Sanitizer Command Response (Unknown/Parse Error) ---\n" + message.payload.hex()
            if serial in tab_widgets and 'log_text' in tab_widgets[serial]:
                log_status(tab_widgets, serial, msg, "blue")
    except Exception as e:
        print(f"Error in on_message: {e}")
        if 'serial' in locals() and serial in tab_widgets and 'log_text' in tab_widgets[serial]:
            log_status(tab_widgets, serial, f"MQTT error: {e}", "red")

def send_command(serial, category, cmd_msg):
    topic = build_topic(serial, category, "cmd")
    client.publish(topic, cmd_msg.SerializeToString())

def send_salt_command(serial, category, target_percentage):
    salt_cmd = sanitizer_pb2.SetSanitizerTargetPercentageRequestPayload()
    salt_cmd.target_percentage = target_percentage
    wrapper = sanitizer_pb2.SanitizerRequestPayloads()
    wrapper.set_sanitizer_output_percentage.CopyFrom(salt_cmd)
    msg = sanitizer_pb2.CommandRequestMessage()
    msg.command_uuid = str(uuid.uuid4())
    msg.sanitizer.CopyFrom(wrapper)
    topic = build_topic(serial, category, "cmd")
    client.publish(topic, msg.SerializeToString())

def background_sender(serial):
    while not device_config[serial]['stop_event'].is_set():
        cfg = device_config[serial]
        send_salt_command(serial, active_devices[serial]['category'], cfg['level'])
        log_status(tab_widgets, serial, f"Background sender: Set power level {cfg['level']}", "orange")
        time.sleep(cfg['interval'])

def update_device_tab(tab_widgets, serial):
    cfg = device_config[serial]
    bg_var = tab_widgets[serial]['bg_var']
    bg_var.set("ON" if cfg['sending'] else "OFF")

def gui_main():
    VERSION = "v2.3.2"
    root = tk.Tk()
    root.title(f"MQTT Device Manager (Notebook Style) - {VERSION}")
    root.geometry("1000x1000")
    root.update_idletasks()
    root.deiconify()

    notebook = ttk.Notebook(root)
    notebook.pack(fill=tk.BOTH, expand=True)

    # Add a placeholder tab for waiting
    waiting_frame = ttk.Frame(notebook)
    notebook.add(waiting_frame, text="WAITING for MQTT")
    waiting_label = ttk.Label(waiting_frame, text="Waiting for first MQTT device announcement...", font=("Arial", 16))
    waiting_label.pack(padx=40, pady=40)

    def create_device_tab(serial):
        # Remove waiting tab if it exists
        for idx in range(len(notebook.tabs())):
            tab_text = notebook.tab(idx, "text")
            if tab_text == "WAITING for MQTT":
                notebook.forget(idx)
                break

        # Pick a unique color for each serial (cycling through a palette)
        color_palette = [
            "#b3e0ff",  # light blue
            "#ffd699",  # light orange
            "#c3f7c3",  # light green
            "#f7c3e6",  # light pink
            "#f7f3c3",  # light yellow
            "#e0c3f7",  # light purple
            "#f7c3c3",  # light red
        ]
        color_idx = abs(hash(serial)) % len(color_palette)
        tab_color = color_palette[color_idx]
        border_thickness = 4

        # Create a custom style for this serial
        style = ttk.Style()
        style_name = f"{serial}.TFrame"
        style.configure(style_name, background=tab_color, borderwidth=border_thickness, relief="solid")
        frame = ttk.Frame(notebook, style=style_name)
        notebook.add(frame, text=serial)

        tab_widgets[serial] = {}

        # Controls (no telemetry interval or salt slider)
        bg_var = tk.StringVar(value="OFF")
        tab_widgets[serial]['bg_var'] = bg_var

        # Log/Status Box
        log_label = ttk.Label(frame, text="Status / Log:", background=tab_color)
        log_label.grid(row=0, column=0, sticky="nw")
        log_text = tk.Text(frame, height=24, width=100, state="disabled", bg="#fffbe6")
        log_text.grid(row=0, column=1, columnspan=2, sticky="w")
        tab_widgets[serial]['log_text'] = log_text

        # Dynamic Command Buttons
        cmd_frame = ttk.LabelFrame(frame, text="Commands", style=style_name)
        cmd_frame.grid(row=1, column=0, columnspan=3, sticky="we", pady=10)

        # Determine device type
        dev_type = get_device_type(serial)
        category = active_devices[serial]["category"].lower()
        is_dct = dev_type == "icl"

        cmd_row = 0
        if is_dct:
            # --- DCT/ICL commands ---
            cmd_msg_type = digitalControllerTransformer_pb2.CommandRequestMessage
            icl_descriptor = digitalControllerTransformer_pb2.DCTRequests.DESCRIPTOR

            for field in icl_descriptor.fields:
                sub_descriptor = field.message_type
                btn_text = field.name.replace("_", " ").title()

                # Special handling for SetLightConfigurationRequest (repeated patch)
                if field.name == "set_dct20_lights":
                    patch_rows = []

                    patch_frame = tk.Frame(cmd_frame)
                    patch_frame.grid(row=cmd_row, column=0, columnspan=3, sticky="we", padx=2, pady=2)

                    def add_patch_row():
                        row = {}
                        row_frame = tk.Frame(patch_frame, relief="groove", borderwidth=1)
                        row_frame.pack(anchor="w", pady=2, fill="x")
                        # Address
                        tk.Label(row_frame, text="address").grid(row=0, column=0)
                        row['address_var'] = tk.StringVar()
                        tk.Entry(row_frame, textvariable=row['address_var'], width=6).grid(row=0, column=1)
                        # time_to_start
                        tk.Label(row_frame, text="time_to_start").grid(row=0, column=2)
                        row['time_var'] = tk.StringVar()
                        tk.Entry(row_frame, textvariable=row['time_var'], width=12).grid(row=0, column=3)
                        # Fields (list)
                        row['fields'] = []
                        fields_frame = tk.Frame(row_frame)
                        fields_frame.grid(row=1, column=0, columnspan=5, sticky="w")
                        def add_field_row():
                            f_row = {}
                            idx = len(row['fields'])
                            f_row['type_var'] = tk.StringVar(value="control_type")
                            f_row['val_var'] = tk.StringVar()
                            tk.OptionMenu(fields_frame, f_row['type_var'], "control_type", "brightness", "drive_mode").grid(row=idx, column=0)
                            tk.Entry(fields_frame, textvariable=f_row['val_var'], width=10).grid(row=idx, column=1)
                            # Remove field button
                            def remove_field():
                                for widget in fields_frame.grid_slaves(row=idx):
                                    widget.grid_forget()
                                row['fields'][idx] = None
                            tk.Button(fields_frame, text="Remove", command=remove_field).grid(row=idx, column=2)
                            row['fields'].append(f_row)
                        tk.Button(row_frame, text="Add Field", command=add_field_row).grid(row=0, column=4)
                        # Remove patch button
                        def remove_patch():
                            row_frame.pack_forget()
                            patch_rows.remove(row)
                        tk.Button(row_frame, text="Remove Patch", command=remove_patch).grid(row=0, column=5)
                        patch_rows.append(row)

                    def send_set_light_configuration():
                        cmd_msg = digitalControllerTransformer_pb2.CommandRequestMessage()
                        cmd_msg.command_uuid = str(uuid.uuid4())
                        set_req = getattr(cmd_msg.icl, field.name)
                        for row in patch_rows:
                            if not row: continue
                            patch = set_req.light_patch.add()
                            patch.address = int(row['address_var'].get())
                            patch.time_to_start = int(row['time_var'].get()) if row['time_var'].get() else 0
                            for f_row in row['fields']:
                                if not f_row: continue
                                field_patch = patch.fields.add()
                                t = f_row['type_var'].get()
                                v = f_row['val_var'].get()
                                if t == "control_type":
                                    field_patch.control_type = int(v)
                                elif t == "brightness":
                                    field_patch.brightness = int(v)
                                elif t == "drive_mode":
                                    # For demo: expects drive_mode as int, real code should build a LightDriveMode message
                                    field_patch.drive_mode = int(v)
                        send_command(serial, active_devices[serial]['category'], cmd_msg)
                        log_status(tab_widgets, serial, "Sent Set Light Configuration", "purple")

                    tk.Button(patch_frame, text="Add Patch", command=add_patch_row).pack(side="left", padx=2)
                    tk.Button(patch_frame, text="Send Set Light Configuration", command=send_set_light_configuration).pack(side="left", padx=2)
                    cmd_row += 1
                    continue

                param_vars = []
                border_frame = tk.Frame(cmd_frame, highlightbackground="#aaa", highlightthickness=1, bd=0)
                border_frame.grid(row=cmd_row, column=0, sticky="we", padx=2, pady=2)
                border_frame.grid_columnconfigure(0, weight=0)
                border_frame.grid_columnconfigure(1, weight=1)

                # Dynamically create input fields for each subfield
                for i, sub_field in enumerate(sub_descriptor.fields):
                    param_var = tk.StringVar()
                    param_vars.append((sub_field, param_var))
                    param_label = ttk.Label(border_frame, text=sub_field.name)
                    param_label.grid(row=0, column=1 + 2 * i, sticky="w", padx=(10,2))
                    param_entry = ttk.Entry(border_frame, textvariable=param_var, width=12)
                    param_entry.grid(row=0, column=2 + 2 * i, sticky="w", padx=(0,8))

                def make_cmd_callback(field, param_vars):
                    def callback():
                        cmd_msg = cmd_msg_type()
                        cmd_msg.command_uuid = str(uuid.uuid4())
                        sub_msg = getattr(cmd_msg.icl, field.name)
                        if len(field.message_type.fields) == 0:
                            sub_msg.SetInParent()
                        else:
                            for sub_field, var in param_vars:
                                value = var.get()
                                try:
                                    if sub_field.type in (sub_field.TYPE_INT32, sub_field.TYPE_INT64, sub_field.TYPE_UINT32, sub_field.TYPE_UINT64, sub_field.TYPE_SINT32, sub_field.TYPE_SINT64, sub_field.TYPE_FIXED32, sub_field.TYPE_FIXED64, sub_field.TYPE_SFIXED32, sub_field.TYPE_SFIXED64):
                                        value = int(value)
                                    elif sub_field.type in (sub_field.TYPE_FLOAT, sub_field.TYPE_DOUBLE):
                                        value = float(value)
                                    elif sub_field.type == sub_field.TYPE_BOOL:
                                        value = value.lower() in ("true", "1", "yes", "on")
                                except Exception:
                                    pass
                                # Fix: handle repeated fields properly
                                if sub_field.label == sub_field.LABEL_REPEATED:
                                    # For simple repeated fields, split by comma and add each
                                    for v in value.split(","):
                                        v = v.strip()
                                        if v:
                                            getattr(sub_msg, sub_field.name).append(type(getattr(sub_msg, sub_field.name)[0])(v) if getattr(sub_msg, sub_field.name) else v)
                                else:
                                    setattr(sub_msg, sub_field.name, value)
                            sub_msg.SetInParent()
                        send_command(serial, active_devices[serial]['category'], cmd_msg)
                        log_status(tab_widgets, serial, f"ICL Sent command: {field.name} with params {', '.join([f'{sf.name}={v.get()}' for sf, v in param_vars])}", "purple")
                    return callback

                cmd_btn = tk.Button(
                    border_frame,
                    text=btn_text,
                    command=make_cmd_callback(field, param_vars),
                    bg="#e0e0ff",
                    fg="black"
                )
                cmd_btn.grid(row=0, column=0, padx=4, pady=2, sticky="w")

                cmd_row += 1
        else:
            # --- Sanitizer-specific: Power Level Button ---
            power_var = tk.StringVar(value=str(device_config[serial]['level']))

            def send_chlorination():
                try:
                    value = int(power_var.get())
                    device_config[serial]['level'] = value  # update for background sender
                except Exception:
                    log_status(tab_widgets, serial, "Invalid Level value", "red")
                    return
                send_salt_command(serial, active_devices[serial]['category'], value)
                log_status(tab_widgets, serial, f"Set Power Level to {value}", "purple")

            border_frame = tk.Frame(cmd_frame, highlightbackground="#aaa", highlightthickness=1, bd=0)
            border_frame.grid(row=cmd_row, column=0, sticky="we", padx=2, pady=2)
            border_frame.grid_columnconfigure(0, weight=0)
            border_frame.grid_columnconfigure(1, weight=1)

            power_label = ttk.Label(border_frame, text="Power Level (%)")
            power_label.grid(row=0, column=1, sticky="w", padx=(10,2))
            power_entry = ttk.Entry(border_frame, textvariable=power_var, width=12)
            power_entry.grid(row=0, column=2, sticky="w", padx=(0,8))
            power_btn = tk.Button(
                border_frame,
                text="Set Power Level",
                command=send_chlorination,
                bg="#e0e0ff",
                fg="black"
            )
            power_btn.grid(row=0, column=0, padx=4, pady=2, sticky="w")
            cmd_row += 1

            # --- Sanitizer/common commands ---
            cmd_msg_type = commonClientMessages_pb2.CommandRequestMessage
            common_descriptor = cmd_msg_type().common.DESCRIPTOR

            for field in common_descriptor.fields:
                sub_descriptor = field.message_type
                btn_text = field.name.replace("_", " ").title()
                param_vars = []

                border_frame = tk.Frame(cmd_frame, highlightbackground="#aaa", highlightthickness=1, bd=0)
                border_frame.grid(row=cmd_row, column=0, sticky="we", padx=2, pady=2)
                border_frame.grid_columnconfigure(0, weight=0)
                border_frame.grid_columnconfigure(1, weight=1)

                def make_cmd_callback(field, param_vars):
                    def callback():
                        cmd_msg = cmd_msg_type()
                        cmd_msg.command_uuid = str(uuid.uuid4())
                        sub_msg = getattr(cmd_msg.common, field.name)
                        if len(field.message_type.fields) == 0:
                            sub_msg.SetInParent()
                        else:
                            for i, sub_field in enumerate(field.message_type.fields):
                                value = param_vars[i].get()
                                try:
                                    if sub_field.type in (sub_field.TYPE_INT32, sub_field.TYPE_INT64, sub_field.TYPE_UINT32, sub_field.TYPE_UINT64, sub_field.TYPE_SINT32, sub_field.TYPE_SINT64, sub_field.TYPE_FIXED32, sub_field.TYPE_FIXED64, sub_field.TYPE_SFIXED32, sub_field.TYPE_SFIXED64):
                                        value = int(value)
                                    elif sub_field.type in (sub_field.TYPE_FLOAT, sub_field.TYPE_DOUBLE):
                                        value = float(value)
                                    elif sub_field.type == sub_field.TYPE_BOOL:
                                        value = value.lower() in ("true", "1", "yes", "on")
                                except Exception:
                                    pass
                                setattr(sub_msg, sub_field.name, value)
                            sub_msg.SetInParent()
                        send_command(serial, active_devices[serial]['category'], cmd_msg)
                        log_status(tab_widgets, serial, f"Sent command: {field.name} with params {', '.join([v.get() for v in param_vars])}", "purple")
                    return callback

                cmd_btn = tk.Button(
                    border_frame,
                    text=btn_text,
                    command=make_cmd_callback(field, param_vars),
                    bg="#e0e0ff",
                    fg="black"
                )
                cmd_btn.grid(row=0, column=0, padx=4, pady=2, sticky="w")

                for i, sub_field in enumerate(sub_descriptor.fields):
                    param_var = tk.StringVar()
                    param_vars.append(param_var)
                    param_label = ttk.Label(border_frame, text=sub_field.name)
                    param_label.grid(row=0, column=1 + 2 * i, sticky="w", padx=(10,2))
                    param_entry = ttk.Entry(border_frame, textvariable=param_var, width=12)
                    param_entry.grid(row=0, column=2 + 2 * i, sticky="w", padx=(0,8))

                cmd_row += 1

        # Add Background Sender button with color change
        def update_bg_btn_color():
            cfg = device_config[serial]
            if cfg['sending']:
                bg_btn.config(bg="red", fg="white")
            else:
                bg_btn.config(bg="#cccccc", fg="black")

        def do_start_stop_bg():
            cfg = device_config[serial]
            if not cfg['sending']:
                cfg['stop_event'].clear()
                t = threading.Thread(target=background_sender, args=(serial,), daemon=True)
                cfg['thread'] = t
                cfg['sending'] = True
                t.start()
                bg_var.set("ON")
                log_status(tab_widgets, serial, "Background sender started.", "green")
            else:
                cfg['stop_event'].set()
                if cfg['thread']:
                    cfg['thread'].join()
                cfg['sending'] = False
                cfg['thread'] = None
                bg_var.set("OFF")
                log_status(tab_widgets, serial, "Background sender stopped.", "orange")
            update_bg_btn_color()

        bg_btn = tk.Button(frame, text="Start/Stop BG Sender", command=do_start_stop_bg, bg="#cccccc", fg="black")
        bg_btn.grid(row=2, column=2, sticky="w", padx=2, pady=2)
        update_bg_btn_color()

        # Static QUIT button, raised up to always be visible
        quit_btn = tk.Button(frame, text="QUIT", command=root.destroy, bg="red", fg="white")
        quit_btn.grid(row=3, column=2, sticky="e", padx=10, pady=10)

    def refresh_gui():
        with active_lock:
            devs = list(active_devices.keys())
        for serial in devs:
            if serial not in tab_widgets:
                create_device_tab(serial)
        for serial in devs:
            update_device_tab(tab_widgets, serial)
        root.after(500, refresh_gui)

    refresh_gui()
    root.mainloop()

def mqtt_thread():
    client.on_connect = on_connect
    client.on_message = on_message
    client.connect(BROKER_ADDRESS, PORT, 60)
    client.loop_start()
    while True:
        time.sleep(1)

def main():
    threading.Thread(target=mqtt_thread, daemon=True).start()
    gui_main()

if __name__ == "__main__":
    main()
