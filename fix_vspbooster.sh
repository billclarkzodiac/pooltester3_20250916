#!/bin/bash
cd ned/

echo "🔧 Removing all shared type references from vspBooster.pb.go..."

# Remove struct definitions
sed -i '/^type CommandRequestMessage struct/,/^}/d' vspBooster.pb.go
sed -i '/^type CommandResponseMessage struct/,/^}/d' vspBooster.pb.go
sed -i '/^type DeviceErrorMessage struct/,/^}/d' vspBooster.pb.go
sed -i '/^type ActiveErrors struct/,/^}/d' vspBooster.pb.go

# Remove all methods for these types
sed -i '/^func (x \*CommandRequestMessage)/,/^}/d' vspBooster.pb.go
sed -i '/^func (x \*CommandResponseMessage)/,/^}/d' vspBooster.pb.go
sed -i '/^func (x \*DeviceErrorMessage)/,/^}/d' vspBooster.pb.go
sed -i '/^func (x \*ActiveErrors)/,/^}/d' vspBooster.pb.go

# Remove constructor functions
sed -i '/^func.*CommandRequestMessage/,/^}/d' vspBooster.pb.go
sed -i '/^func.*CommandResponseMessage/,/^}/d' vspBooster.pb.go
sed -i '/^func.*DeviceErrorMessage/,/^}/d' vspBooster.pb.go
sed -i '/^func.*ActiveErrors/,/^}/d' vspBooster.pb.go

echo "✅ vspBooster.pb.go cleaned"
