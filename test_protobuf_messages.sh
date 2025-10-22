#!/bin/bash
echo "🧬 Testing Protobuf Message Processing"

# Test different message formats for each device type
echo "Testing Sanitizer protobuf messages..."
mosquitto_pub -h 169.254.1.1 -t "async/sanitizerGen2/PROTO_TEST_001/anc" -m $'binary_protobuf_data_here'

echo "Testing Controller protobuf messages..."  
mosquitto_pub -h 169.254.1.1 -t "async/digitalControllerGen2/PROTO_TEST_002/anc" -m $'binary_protobuf_data_here'

echo "Testing Pump protobuf messages..."
mosquitto_pub -h 169.254.1.1 -t "async/speedsetplus/PROTO_TEST_003/anc" -m $'binary_protobuf_data_here'

echo "✅ Protobuf message tests sent"
