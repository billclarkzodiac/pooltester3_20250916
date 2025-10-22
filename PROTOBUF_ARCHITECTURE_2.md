# Create comprehensive documentation for your successor

# NgaSim Protobuf Architecture

## Device Discovery & Dynamic Commands

### Supported Device Types:
- **Sanitizer Gen2** (`sanitizer.pb.go`) - Pool sanitizers
- **Digital Controller Transformer** (`digitalControllerTransformer.pb.go`) - Pool controllers  
- **SpeedSet Plus** (`speedsetplus.pb.go`) - Variable speed pumps
- **VSP Booster** (`vspBooster.pb.go`) - Booster pumps

### Dynamic Command Generation:
The system uses Go reflection to:
1. **Discover devices** via MQTT announcements
2. **Identify device type** from protobuf messages
3. **Generate commands dynamically** using protobuf reflection
4. **Create web UI** automatically for each device type

### Adding New Device Types:
1. Add new `.proto` and `.pb.go` files to `ned/` directory
2. System automatically discovers and integrates new device types
3. No code changes needed - pure protobuf + reflection

### File Structure:

ned/  
├── commonClientMessages.pb.go # Shared message types  
├── sanitizer.pb.go # Sanitizer-specific messages  
├── digitalControllerTransformer.pb.go # Controller messages  
├── speedsetplus.pb.go # Pump messages  
└── [future device types] # Add new devices here  

