package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Configuration
const serverPort = 3200

// Position in 3D space
type Position struct {
	X float64 `json:"X"`
	Y float64 `json:"Y"`
	Z float64 `json:"Z"`
}

// Node represents an ESP8266 device
type Node struct {
	ID       string   `json:"id"`
	Conn     *websocket.Conn `json:"-"` // Don't include in JSON
	Position Position `json:"position"` // Fixed position of the node
	Distance float64  `json:"distance"` // Measured distance to the target
	RSSI     int      `json:"rssi"`     // Raw RSSI value
	LastSeen time.Time `json:"lastSeen"`
}

// Message types from ESP nodes
type Message struct {
	Type     string  `json:"type"`
	NodeID   string  `json:"node_id"`
	RSSI     int     `json:"rssi"`
	Distance float64 `json:"distance"`
}

// Visualization data structure
type VisualizationData struct {
	Nodes   map[string]Position `json:"nodes"`
	Clients map[string]Position `json:"clients"`
}

// Calibration parameters
type CalibrationParams struct {
	RSSIAt1m float64 `json:"rssi_at_1m"` // RSSI value at 1 meter
	PathLoss float64 `json:"path_loss"`  // Path loss exponent (typically 2.0-4.0)
}

// Global variables
var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins
	}

	nodes        = make(map[string]*Node)
	nodesMutex   sync.RWMutex
	logger       = log.New(os.Stdout, "", log.LstdFlags)
	
	// Phone position (target device)
	phonePosition = Position{X: 0, Y: 0, Z: 0}
	
	// Default calibration parameters (can be updated via API)
	calibration = map[string]CalibrationParams{
		"default": {RSSIAt1m: -60.0, PathLoss: 2.0},
	}
)

// WebSocket handler function
func wsHandler(w http.ResponseWriter, r *http.Request) {
	logger.Printf("New connection request from %s", r.RemoteAddr)
	
	// Log all request headers for debugging
	logger.Println("Request headers:")
	for name, values := range r.Header {
		for _, value := range values {
			logger.Printf("  %s: %s", name, value)
		}
	}
	
	// More permissive upgrader for debugging
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	upgrader.EnableCompression = false
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Printf("Error upgrading connection: %v", err)
		return
	}

	// Get the IP address and create a consistent ID from it
	remoteAddr := r.RemoteAddr
	ipAddress := remoteAddr[:strings.LastIndex(remoteAddr, ":")]
	// Clean the IP address for use as an ID (remove dots and colons)
	cleanIP := strings.ReplaceAll(strings.ReplaceAll(ipAddress, ".", "_"), ":", "_")
	tempID := fmt.Sprintf("ESP_%s", cleanIP)

	// Create new node
	newNode := &Node{
		ID:       tempID,
		Conn:     conn,
		Position: Position{X: 0, Y: 0, Z: 0}, // Will be configured later
		Distance: 0,
		LastSeen: time.Now(),
	}

	// Store node
	nodesMutex.Lock()
	nodes[tempID] = newNode
	nodesMutex.Unlock()

	logger.Printf("Client connected: %s", tempID)

	// Set ping handler
	conn.SetPingHandler(func(appData string) error {
		logger.Printf("Received ping from %s", tempID)
		return conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
	})

	// Handle messages from this connection
	handleMessages(conn, tempID)
}

func handleMessages(conn *websocket.Conn, nodeID string) {
	// Check if we already have a node with this ID
	nodesMutex.Lock()
	existingNode, exists := nodes[nodeID]
	if exists {
		// If the node exists but has a different connection, close the old one
		if existingNode.Conn != conn {
			logger.Printf("Node %s reconnected with new connection", nodeID)
			existingNode.Conn.Close()
			existingNode.Conn = conn
		}
	}
	nodesMutex.Unlock()

	defer func() {
		logger.Printf("Closing connection for %s", nodeID)
		conn.Close()

		// We no longer automatically remove the node from the map
		// This allows the node to reconnect while maintaining its position and calibration
		// Instead, we just mark it as disconnected by setting Conn to nil
		nodesMutex.Lock()
		if node, nodeExists := nodes[nodeID]; nodeExists && node.Conn == conn {
			// Only set to nil if this is still the active connection
			node.Conn = nil
			logger.Printf("Node %s marked as disconnected", nodeID)
		}
		nodesMutex.Unlock()
	}()

	// Wait a moment before sending first message
	time.Sleep(500 * time.Millisecond)
	
	// Send ID immediately after connection
	logger.Printf("Sending ID to %s", nodeID)
	err := conn.WriteMessage(websocket.TextMessage, []byte("ID:"+nodeID))
	if err != nil {
		logger.Printf("Error sending ID to node %s: %v", nodeID, err)
		return
	}
	
	logger.Printf("ID sent to %s", nodeID)

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			logger.Printf("Read error from %s: %v", nodeID, err)
			break
		}

		// Update last seen timestamp
		nodesMutex.Lock()
		if node, exists := nodes[nodeID]; exists {
			node.LastSeen = time.Now()
		}
		nodesMutex.Unlock()

		// Try to parse as JSON
		var msg Message
		jsonErr := json.Unmarshal(message, &msg)
		
		if jsonErr == nil {
			// Handle structured message
			switch msg.Type {
			case "distance":
				logger.Printf("Received distance data from %s: RSSI=%d, Distance=%.2fm", nodeID, msg.RSSI, msg.Distance)
				
				nodesMutex.Lock()
				if node, exists := nodes[nodeID]; exists {
					node.RSSI = msg.RSSI
					node.Distance = msg.Distance
				}
				nodesMutex.Unlock()
				
				// Recalculate phone position
				updatePhonePosition()
				
			case "calibration":
				logger.Printf("Received calibration data from %s", nodeID)
				// Handle calibration data
				
			case "position":
				// Handle node reporting its position
				var pos Position
				if err := json.Unmarshal([]byte(message), &pos); err == nil {
					nodesMutex.Lock()
					if node, exists := nodes[nodeID]; exists {
						node.Position = pos
						logger.Printf("Updated position for %s: (%.2f, %.2f, %.2f)", 
							nodeID, pos.X, pos.Y, pos.Z)
					}
					nodesMutex.Unlock()
				}
			}
		} else {
			// Handle as plain text message
			messageStr := string(message)
			logger.Printf("Received text message from %s: %s", nodeID, messageStr)

			// Echo the message back
			if messageStr == "PING" {
				logger.Printf("Sending PONG to %s", nodeID)
				if err := conn.WriteMessage(messageType, []byte("PONG")); err != nil {
					logger.Printf("Error sending PONG to %s: %v", nodeID, err)
					break
				}
			} else if messageStr == "REGISTER" {
				logger.Printf("Got REGISTER from %s, sending ID confirmation", nodeID)
				if err := conn.WriteMessage(websocket.TextMessage, []byte("ID:"+nodeID)); err != nil {
					logger.Printf("Error sending ID confirmation to %s: %v", nodeID, err)
					break
				}
			}
		}
	}
}

// Calculate distance from RSSI
func calculateDistanceFromRSSI(rssi int, nodeID string) float64 {
	params, ok := calibration[nodeID]
	if !ok {
		params = calibration["default"]
	}
	
	return math.Pow(10, (params.RSSIAt1m - float64(rssi)) / (10 * params.PathLoss))
}

// Trilateration algorithm to determine phone position
func updatePhonePosition() {
	nodesMutex.RLock()
	defer nodesMutex.RUnlock()
	
	// Need at least 3 nodes with distances for 3D trilateration
	var validNodes []*Node
	for _, node := range nodes {
		if node.Distance > 0 {
			validNodes = append(validNodes, node)
		}
	}
	
	if len(validNodes) < 3 {
		logger.Printf("Not enough nodes with distance measurements for trilateration: %d", len(validNodes))
		return
	}
	
	// Use non-linear least squares algorithm for trilateration
	// Starting with a guess at the center of the system
	initialGuess := Position{X: 0, Y: 0, Z: 0}
	
	// Simple implementation using gradient descent
	// In a production system, you'd use a more robust solver
	position := trilateratePosition(validNodes, initialGuess)
	
	logger.Printf("Updated phone position: (%.2f, %.2f, %.2f)", position.X, position.Y, position.Z)
	phonePosition = position
}

// Trilateration using gradient descent
func trilateratePosition(nodes []*Node, initialGuess Position) Position {
	// Implementation parameters
	maxIterations := 100
	learningRate := 0.1
	convergenceThreshold := 0.001
	
	position := initialGuess
	
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Calculate gradient
		gradient := Position{X: 0, Y: 0, Z: 0}
		totalError := 0.0
		
		for _, node := range nodes {
			// Calculate actual distance from current estimated position to the node
			dx := position.X - node.Position.X
			dy := position.Y - node.Position.Y
			dz := position.Z - node.Position.Z
			calculatedDistance := math.Sqrt(dx*dx + dy*dy + dz*dz)
			
			// Error is the difference between calculated and measured distance
			error := calculatedDistance - node.Distance
			totalError += error * error
			
			// Calculate gradient components
			if calculatedDistance > 0 {
				gradient.X += 2 * error * dx / calculatedDistance
				gradient.Y += 2 * error * dy / calculatedDistance
				gradient.Z += 2 * error * dz / calculatedDistance
			}
		}
		
		// Update position using gradient descent
		position.X -= learningRate * gradient.X
		position.Y -= learningRate * gradient.Y
		position.Z -= learningRate * gradient.Z
		
		// Check for convergence
		gradientMagnitude := math.Sqrt(gradient.X*gradient.X + gradient.Y*gradient.Y + gradient.Z*gradient.Z)
		if gradientMagnitude < convergenceThreshold {
			logger.Printf("Trilateration converged after %d iterations, error: %.6f", iteration, totalError)
			break
		}
		
		// If we reach the last iteration without converging
		if iteration == maxIterations-1 {
			logger.Printf("Trilateration did not fully converge after %d iterations, error: %.6f", maxIterations, totalError)
		}
	}
	
	return position
}

// Handler for calibration
func calibrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Parse input
	var input struct {
		NodeID     string  `json:"node_id"`
		RSSIAt1m   float64 `json:"rssi_at_1m"`
		PathLoss   float64 `json:"path_loss"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Update calibration for this node
	nodesMutex.Lock()
	calibration[input.NodeID] = CalibrationParams{
		RSSIAt1m: input.RSSIAt1m,
		PathLoss: input.PathLoss,
	}
	nodesMutex.Unlock()
	
	logger.Printf("Updated calibration for node %s: RSSI@1m=%.2f, PathLoss=%.2f", 
		input.NodeID, input.RSSIAt1m, input.PathLoss)
	
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// Handler for visualization data
func visualizationHandler(w http.ResponseWriter, r *http.Request) {
	nodesMutex.RLock()
	defer nodesMutex.RUnlock()
	
	// Prepare node positions
	nodePositions := make(map[string]Position)
	for id, node := range nodes {
		nodePositions[id] = node.Position
	}
	
	// Prepare visualization data
	data := VisualizationData{
		Nodes: nodePositions,
		Clients: map[string]Position{
			"PHONE": phonePosition,
		},
	}
	
	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow cross-origin requests
	
	// Send JSON response
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Printf("Error encoding visualization data: %v", err)
		http.Error(w, "Error encoding data", http.StatusInternalServerError)
		return
	}
}

// Handler for setting node positions manually (for calibration)
func setNodePositionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Parse input
	var input struct {
		NodeID string   `json:"node_id"`
		Position Position `json:"position"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Update node position
	nodesMutex.Lock()
	node, exists := nodes[input.NodeID]
	if !exists {
		nodesMutex.Unlock()
		http.Error(w, "Node not found", http.StatusNotFound)
		return
	}
	
	node.Position = input.Position
	nodesMutex.Unlock()
	
	logger.Printf("Set position for node %s: (%.2f, %.2f, %.2f)", 
		input.NodeID, input.Position.X, input.Position.Y, input.Position.Z)
	
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// Print status of all nodes
func printNodesStatus() {
	nodesMutex.RLock()
	defer nodesMutex.RUnlock()
	
	logger.Println("\n=== Node Status ===")
	logger.Printf("Total nodes: %d", len(nodes))
	
	for id, node := range nodes {
		status := "DISCONNECTED"
		if node.Conn != nil {
			status = "CONNECTED"
		}
		
		timeSinceLastSeen := time.Since(node.LastSeen)
		logger.Printf("Node %s: %s, Position: (%.2f, %.2f, %.2f), Last seen: %s ago", 
			id, status, node.Position.X, node.Position.Y, node.Position.Z, 
			timeSinceLastSeen.Round(time.Second))
	}
	
	if len(nodes) > 0 {
		logger.Printf("Phone position: (%.2f, %.2f, %.2f)", 
			phonePosition.X, phonePosition.Y, phonePosition.Z)
	}
	logger.Println("==================")
}

// Simple HTML page for testing
func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// Serve visualization.html
		if r.URL.Path == "/visualization.html" {
			http.ServeFile(w, r, "visualization.html")
			return
		}
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Test</title>
    <script>
        let socket;
        
        function connect() {
            socket = new WebSocket("ws://" + window.location.host + "/ws");
            
            socket.onopen = function(e) {
                console.log("WebSocket connected!");
                document.getElementById("status").textContent = "Connected";
                document.getElementById("status").style.color = "green";
            };
            
            socket.onmessage = function(event) {
                console.log("Message from server: ", event.data);
                let messagesDiv = document.getElementById("messages");
                let messageElem = document.createElement("div");
                messageElem.textContent = "← " + event.data;
                messagesDiv.appendChild(messageElem);
            };
            
            socket.onclose = function(event) {
                console.log("WebSocket disconnected!");
                document.getElementById("status").textContent = "Disconnected";
                document.getElementById("status").style.color = "red";
            };
            
            socket.onerror = function(error) {
                console.log("WebSocket error: ", error);
                document.getElementById("status").textContent = "Error";
                document.getElementById("status").style.color = "red";
            };
        }
        
        function sendMessage() {
            let messageInput = document.getElementById("messageInput");
            let message = messageInput.value;
            
            if (message && socket) {
                socket.send(message);
                
                let messagesDiv = document.getElementById("messages");
                let messageElem = document.createElement("div");
                messageElem.textContent = "→ " + message;
                messagesDiv.appendChild(messageElem);
                
                messageInput.value = "";
            }
        }

        function openVisualization() {
            window.open("/visualization.html", "_blank");
        }
        
        window.onload = connect;
    </script>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 20px;
        }
        #messages {
            border: 1px solid #ccc;
            padding: 10px;
            height: 300px;
            overflow-y: auto;
            margin-bottom: 10px;
        }
        #controls {
            display: flex;
            margin-bottom: 10px;
        }
        #messageInput {
            flex-grow: 1;
            margin-right: 10px;
            padding: 5px;
        }
        #status {
            margin-top: 10px;
            font-weight: bold;
        }
        button {
            padding: 5px 10px;
            margin-right: 10px;
        }
    </style>
</head>
<body>
    <h1>ESP8266 Trilateration Debug</h1>
    <div id="messages"></div>
    <div id="controls">
        <input type="text" id="messageInput" placeholder="Type a message...">
        <button onclick="sendMessage()">Send</button>
    </div>
    <div>
        <button onclick="openVisualization()">Open 3D Visualization</button>
    </div>
    <div id="status">Connecting...</div>
</body>
</html>
	`)
}

// Main function
func main() {
	// Start a goroutine to periodically print node status
	go func() {
		for {
			time.Sleep(30 * time.Second)
			printNodesStatus()
		}
	}()

	// Set up HTTP handlers
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/visualization", visualizationHandler)
	http.HandleFunc("/set-node-position", setNodePositionHandler)
	http.HandleFunc("/calibrate", calibrationHandler)

	logger.Printf("Starting trilateration server on port %d", serverPort)
	logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", serverPort), nil))
}
