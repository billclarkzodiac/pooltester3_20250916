# NgaSim Pool Controller - TODO List

## 🔧 **Pending Fixes & Enhancements**

### **Serial Number Sorting** 
- **Issue:** Devices may appear in discovery order rather than sequential serial order
- **Details:** 18-character ASCII serial numbers should sort character-by-character (lexicographic)
- **Current:** Basic string sorting works, but discovery timing affects initial display order
- **Priority:** Low (system works fine, cosmetic improvement)
- **Solution:** Enhance sort logic in `handlers.go` `handleRoot()` and `handleDevices()`

### **Protobuf Parser Enhancements**
- **Current:** Basic parsing with placeholder fields
- **Future:** 
  - [ ] Add actual protobuf schema parsing when `ned` package is available
  - [ ] Enhanced field type detection (temperature, voltage, percentages)
  - [ ] Unit conversion and formatting
  - [ ] Field validation and ranges

### **UI/UX Improvements**
- **Terminal:** 
  - [ ] Add terminal search/filter functionality
  - [ ] Export terminal logs to file
  - [ ] Real-time terminal updates via WebSocket
- **Device Cards:**
  - [ ] Collapsible device terminals
  - [ ] Device grouping by type
  - [ ] Favorite/bookmark devices
- **Forms:**
  - [ ] Form validation for protobuf commands
  - [ ] Command history and favorites
  - [ ] Bulk command execution

### **System Features**
- **Monitoring:**
  - [ ] Device health monitoring and alerts
  - [ ] Performance metrics and charts
  - [ ] Historical data logging
- **Configuration:**
  - [ ] Device configuration management
  - [ ] User preferences and settings
  - [ ] Multi-user access control
- **Integration:**
  - [ ] REST API documentation
  - [ ] WebHook support for external systems
  - [ ] MQTT broker management interface

### **Code Quality**
- **Error Handling:**
  - [ ] Better error recovery for MQTT disconnections
  - [ ] Graceful degradation when protobuf parsing fails
  - [ ] User-friendly error messages
- **Testing:**
  - [ ] Unit tests for core functions
  - [ ] Integration tests for MQTT handling
  - [ ] Load testing with multiple devices
- **Documentation:**
  - [ ] API documentation
  - [ ] User guide for protobuf commands
  - [ ] Developer setup instructions

## ✅ **Completed Features**

### **v1.0 - Basic Functionality**
- [x] MQTT device discovery and monitoring
- [x] Basic device control interface
- [x] Device status display
- [x] Simple terminal logging

### **v2.0 - Enhanced Protobuf Integration** 
- [x] Protobuf message parsing framework
- [x] Enhanced device terminals with structured data display
- [x] ParsedProtobuf fields in TerminalEntry
- [x] Rich terminal display showing message fields and descriptions
- [x] Device sorting by serial number
- [x] Dynamic web interface with live updates

## 🎯 **Next Milestones**

### **Short Term** (Next Session)
1. Serial number sorting refinement (if needed)
2. Terminal search/filter functionality
3. Better error handling for edge cases

### **Medium Term** (Next Few Sessions)
1. WebSocket real-time updates
2. Device health monitoring
3. Enhanced protobuf schema integration

### **Long Term** (Future Development)
1. Multi-user system with authentication
2. Historical data analysis and charts
3. Advanced device automation and scheduling

---
**Last Updated:** $(date)
**Current Version:** v2.0 - Enhanced Protobuf Integration
**Status:** ✅ Core functionality working, ready for next enhancements
