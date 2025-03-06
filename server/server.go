package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Port to listen on
	serverPort = 3200
	
	// Maximum number of nodes in the mesh
	maxNodes = 10
)

// Node represents a connected ESP8266 device
type Node struct {
	ID         int       `json:"id"`
	MacAddress string    `json:"mac"`
	LastSeen   time.Time `json:"last_seen"`
	Connection *websocket.Conn
}

// RSSIMeasurement represents a single RSSI measurement between two nodes
type RSSIMeasurement struct {
	SourceID  int       `json:"source_id"`
	TargetID  int       `json:"target_id"`
	RSSI      int       `json:"rssi"`
	Timestamp time.Time `json:"timestamp"`
}

// Message types for WebSocket communication
type registrationMessage struct {
	Type       string `json:"type"`
	MacAddress string `json:"mac"`
}

type idAssignmentMessage struct {
	Type   string `json:"type"`
	ID     int    `json:"id"`
}

type nodeListMessage struct {
	Type  string     `json:"type"`
	Nodes []nodeInfo `json:"nodes"`
}

type nodeInfo struct {
	ID  int    `json:"id"`
	Mac string `json:"mac"`
}

type rssiReportMessage struct {
	Type         string         `json:"type"`
	NodeID       int            `json:"node_id"`
	Timestamp    int64          `json:"timestamp"`
	Measurements []measurement  `json:"measurements"`
}

type measurement struct {
	TargetID int `json:"target_id"`
	RSSI     int `json:"rssi"`
}

// Server state
var (
	// Mutex for protecting the nodes map
	mu sync.Mutex
	
	// Map of node ID to Node
	nodes = make(map[int]*Node)
	
	// Next available node ID
	nextNodeID = 1
	
	// WebSocket upgrader
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// Allow all origins for development
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	
	// Channel for RSSI measurements
	rssiChan = make(chan RSSIMeasurement, 1000)
)

func main() {
	// Start RSSI processor
	go processRSSIMeasurements()
	
	// Set up WebSocket endpoint
	http.HandleFunc("/ws", handleWebSocket)
	
	// Set up status endpoint
	http.HandleFunc("/status", handleStatus)
	
	// Start the server
	addr := fmt.Sprintf(":%d", serverPort)
	log.Printf("Server starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading connection: %v", err)
		return
	}
	
	log.Printf("New WebSocket connection from %s", r.RemoteAddr)
	
	// Handle the connection in a goroutine
	go handleNode(conn)
}

func handleNode(conn *websocket.Conn) {
	// Close the connection when this function returns
	defer func() {
		conn.Close()
		log.Printf("Connection closed")
	}()
	
	// Node ID will be assigned upon registration
	var nodeID int = -1
	
	// Read messages from the connection
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		
		// Parse the message
		var msgMap map[string]interface{}
		if err := json.Unmarshal(message, &msgMap); err != nil {
			log.Printf("Error parsing message: %v", err)
			continue
		}
		
		// Check message type
		msgType, ok := msgMap["type"].(string)
		if !ok {
			log.Printf("Message missing 'type' field")
			continue
		}
		
		switch msgType {
		case "register":
			// Handle node registration
			var regMsg registrationMessage
			if err := json.Unmarshal(message, &regMsg); err != nil {
				log.Printf("Error parsing registration message: %v", err)
				continue
			}
			
			// Assign a node ID
			nodeID = assignNodeID(regMsg.MacAddress, conn)
			
			// Send the node its ID
			idMsg := idAssignmentMessage{
				Type: "id_assignment",
				ID:   nodeID,
			}
			if err := conn.WriteJSON(idMsg); err != nil {
				log.Printf("Error sending ID assignment: %v", err)
				return
			}
			
			// Broadcast updated node list to all nodes
			broadcastNodeList()
			
		case "rssi_report":
			// Handle RSSI report
			var rssiMsg rssiReportMessage
			if err := json.Unmarshal(message, &rssiMsg); err != nil {
				log.Printf("Error parsing RSSI report: %v", err)
				continue
			}
			
			// Process the RSSI measurements
			processRSSIReport(rssiMsg)
		}
	}
	
	// If the node was registered, remove it when the connection closes
	if nodeID > 0 {
		removeNode(nodeID)
	}
}

func assignNodeID(macAddress string, conn *websocket.Conn) int {
	mu.Lock()
	defer mu.Unlock()
	
	// Check if this MAC address already has a node ID
	for id, node := range nodes {
		if node.MacAddress == macAddress {
			// Update the connection and last seen time
			node.Connection = conn
			node.LastSeen = time.Now()
			log.Printf("Existing node reconnected: ID %d, MAC %s", id, macAddress)
			return id
		}
	}
	
	// Assign a new node ID
	id := nextNodeID
	nextNodeID++
	
	// Create a new node
	nodes[id] = &Node{
		ID:         id,
		MacAddress: macAddress,
		LastSeen:   time.Now(),
		Connection: conn,
	}
	
	log.Printf("New node registered: ID %d, MAC %s", id, macAddress)
	return id
}

func removeNode(id int) {
	mu.Lock()
	defer mu.Unlock()
	
	// Remove the node from the map
	if node, ok := nodes[id]; ok {
		log.Printf("Node disconnected: ID %d, MAC %s", id, node.MacAddress)
		delete(nodes, id)
	}
	
	// Broadcast updated node list
	broadcastNodeList()
}

func broadcastNodeList() {
	mu.Lock()
	defer mu.Unlock()
	
	// Create the node list message
	var nodeList []nodeInfo
	for id, node := range nodes {
		nodeList = append(nodeList, nodeInfo{
			ID:  id,
			Mac: node.MacAddress,
		})
	}
	
	msg := nodeListMessage{
		Type:  "node_list",
		Nodes: nodeList,
	}
	
	// Send the message to all connected nodes
	for id, node := range nodes {
		if err := node.Connection.WriteJSON(msg); err != nil {
			log.Printf("Error sending node list to node %d: %v", id, err)
		}
	}
	
	log.Printf("Broadcast node list to %d nodes", len(nodes))
}

func processRSSIReport(report rssiReportMessage) {
	// Convert report timestamp to server time
	reportTime := time.Now()
	
	// Process each measurement
	for _, m := range report.Measurements {
		measurement := RSSIMeasurement{
			SourceID:  report.NodeID,
			TargetID:  m.TargetID,
			RSSI:      m.RSSI,
			Timestamp: reportTime,
		}
		
		// Send to processing channel
		rssiChan <- measurement
	}
}

func processRSSIMeasurements() {
	// This goroutine processes RSSI measurements from the channel
	for measurement := range rssiChan {
		// Here you would store and/or process the measurements
		// For now, we'll just log them
		log.Printf("RSSI: Node %d -> Node %d: %d dBm at %v",
			measurement.SourceID,
			measurement.TargetID,
			measurement.RSSI,
			measurement.Timestamp.Format(time.RFC3339),
		)
		
		// In a real implementation, you might:
		// - Store in a database
		// - Calculate triangulation
		// - Generate visualization data
		// - etc.
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	
	// Create a simple status response
	type nodeStatus struct {
		ID       int       `json:"id"`
		MAC      string    `json:"mac"`
		LastSeen time.Time `json:"last_seen"`
	}
	
	var status struct {
		Nodes    []nodeStatus `json:"nodes"`
		NodeCount int         `json:"node_count"`
		Uptime   string       `json:"uptime"`
	}
	
	// Fill in node information
	for id, node := range nodes {
		status.Nodes = append(status.Nodes, nodeStatus{
			ID:       id,
			MAC:      node.MacAddress,
			LastSeen: node.LastSeen,
		})
	}
	
	status.NodeCount = len(nodes)
	status.Uptime = time.Since(time.Now()).String() // Just a placeholder
	
	// Write the response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Printf("Error encoding status response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
