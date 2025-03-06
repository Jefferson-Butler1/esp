#!/bin/bash
# Calibration script for the trilateration system

SERVER_IP="192.168.0.100"
SERVER_PORT="3200"

# Function to display help
show_help() {
    echo "ESP8266 Trilateration System Calibration Script"
    echo ""
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  setpos <node_id> <x> <y> <z>     Set the position of a node"
    echo "  calibrate <node_id> [rssi] [pl]  Calibrate RSSI at 1m for a node"
    echo "                                   Optional: specify RSSI value and path loss exponent"
    echo "  status                           Show the status of all nodes"
    echo "  test                             Test the system"
    echo "  viz                              Open visualization in browser"
    echo "  help                             Show this help"
    echo ""
}

# Function to set node position
set_node_position() {
    if [ $# -ne 4 ]; then
        echo "Error: setpos requires node_id, x, y, z coordinates"
        echo "Usage: $0 setpos <node_id> <x> <y> <z>"
        exit 1
    fi

    NODE_ID=$1
    X=$2
    Y=$3
    Z=$4

    echo "Setting position for node $NODE_ID to ($X, $Y, $Z)..."
    
    # Use curl to send the position to the server
    curl -X POST -H "Content-Type: application/json" \
        -d "{\"node_id\": \"$NODE_ID\", \"position\": {\"X\": $X, \"Y\": $Y, \"Z\": $Z}}" \
        http://$SERVER_IP:$SERVER_PORT/set-node-position
        
    echo ""
}

# Function to calibrate RSSI
calibrate_rssi() {
    if [ $# -lt 1 ]; then
        echo "Error: calibrate requires node_id"
        echo "Usage: $0 calibrate <node_id> [rssi_at_1m] [path_loss]"
        exit 1
    fi

    NODE_ID=$1
    
    # Default values if not provided
    RSSI_AT_1M=${2:-"-60"}
    PATH_LOSS=${3:-"2.0"}
    
    echo "==== RSSI Calibration for Node $NODE_ID ===="
    echo ""
    
    if [ $# -eq 1 ]; then
        echo "1. Place your phone exactly 1 meter away from node $NODE_ID"
        echo "2. Make sure your phone's WiFi is turned on"
        echo "3. Keep the phone stationary"
        echo ""
        read -p "Press Enter to start calibration, or Ctrl+C to cancel..."
        
        echo "Please enter the RSSI value measured at 1 meter:"
        read -p "RSSI at 1m (default -60): " user_rssi
        if [ ! -z "$user_rssi" ]; then
            RSSI_AT_1M=$user_rssi
        fi
        
        echo "Please enter the path loss exponent (2.0 for free space, 3.0-4.0 for indoor):"
        read -p "Path loss exponent (default 2.0): " user_pl
        if [ ! -z "$user_pl" ]; then
            PATH_LOSS=$user_pl
        fi
    else
        echo "Using provided values:"
        echo "RSSI at 1m: $RSSI_AT_1M"
        echo "Path loss exponent: $PATH_LOSS"
    fi
    
    echo "Sending calibration data to server..."
    
    # Use curl to send calibration data to the server
    curl -X POST -H "Content-Type: application/json" \
        -d "{\"node_id\": \"$NODE_ID\", \"rssi_at_1m\": $RSSI_AT_1M, \"path_loss\": $PATH_LOSS}" \
        http://$SERVER_IP:$SERVER_PORT/calibrate
    
    echo ""
    echo "Calibration complete!"
    echo ""
}

# Function to show node status
show_status() {
    echo "Retrieving node status..."
    
    # Get visualization data
    VIZ_DATA=$(curl -s http://$SERVER_IP:$SERVER_PORT/visualization)
    
    # Extract and display node information
    echo "Connected nodes:"
    echo "$VIZ_DATA" | python3 -c '
import json, sys
data = json.load(sys.stdin)
for node_id, position in data["nodes"].items():
    print(f"  {node_id}: ({position['X']:.2f}, {position['Y']:.2f}, {position['Z']:.2f})")
phone = data["clients"]["PHONE"]
print(f"\nPhone position: ({phone['X']:.2f}, {phone['Y']:.2f}, {phone['Z']:.2f})")
'
    echo ""
}

# Function to test the system
test_system() {
    echo "Testing the trilateration system..."
    echo "Retrieving current positions from the server..."
    
    # Use curl to get visualization data
    curl -s http://$SERVER_IP:$SERVER_PORT/visualization | \
        python3 -m json.tool
    
    echo ""
    echo "1. Check that all nodes are visible and positioned correctly"
    echo "2. Check that the phone position is being calculated"
    echo ""
    echo "You can also open the visualization in your browser:"
    echo "http://$SERVER_IP:$SERVER_PORT/visualization.html"
    echo ""
}

# Function to open visualization
open_visualization() {
    echo "Opening visualization in browser..."
    open "http://$SERVER_IP:$SERVER_PORT/visualization.html" || \
    xdg-open "http://$SERVER_IP:$SERVER_PORT/visualization.html" || \
    echo "Could not open browser automatically. Please visit: http://$SERVER_IP:$SERVER_PORT/visualization.html"
}

# Main script logic
case "$1" in
    setpos)
        shift
        set_node_position "$@"
        ;;
    calibrate)
        shift
        calibrate_rssi "$@"
        ;;
    status)
        show_status
        ;;
    test)
        test_system
        ;;
    viz)
        open_visualization
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        if [ -z "$1" ]; then
            show_help
        else
            echo "Unknown command: $1"
            echo ""
            show_help
        fi
        ;;
esac