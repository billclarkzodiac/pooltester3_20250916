### NgaSim Quick Reference

## Start Application
```bash
cd /home/test/projects/pooltester3_20250916
./pool-controller &
```

### Test Web Interface
**Main Interface:** http://localhost:8082  
**Device API:** http://localhost:8082/api/devices  

### Test Device Creation
```sh
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/TEST999/anc" -m "test"
```

### Development Rules

1. Make ONE change at a time
2. Test it works
3. Commit if successful
4. Document what you changed

### Current Status: WORKING REVISION ✅

**Perfect!** 🎉 Now you have:
- **DEVELOPMENT_STATUS.md** - Complete project status and lessons learned
- **QUICK_REFERENCE.md** - Daily operational guide
- **Both committed to git** - Preserved for the team

This documentation will be invaluable for:
- **Future you** - Remember what works and what doesn't
- **Team members** - Understand the project history
- **Code reviews** - See the development philosophy

**Great call on saving this!** Documentation like this is what separates professional projects from hobby code. 📚**Perfect!** 🎉 Now you have:
- **DEVELOPMENT_STATUS.md** - Complete project status and lessons learned
- **QUICK_REFERENCE.md** - Daily operational guide
- **Both committed to git** - Preserved for the team

This documentation will be invaluable for:
- **Future you** - Remember what works and what doesn't
- **Team members** - Understand the project history
- **Code reviews** - See the development philosophy

**Great call on saving this!** Documentation like this is what separates professional projects from hobby code. 📚
