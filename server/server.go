package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Configuration
const serverPort = 3200

// Global variables
var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins
	}

	nodes      = make(map[string]*websocket.Conn)
	nodesMutex sync.RWMutex
	nextNodeID = 1
	logger     = log.New(os.Stdout, "", log.LstdFlags)
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

	// Assign a temporary ID
	tempID := fmt.Sprintf("ESP_%d", nextNodeID)
	nextNodeID++

	// Store connection
	nodesMutex.Lock()
	nodes[tempID] = conn
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
	defer func() {
		logger.Printf("Closing connection for %s", nodeID)
		conn.Close()

		// Remove node from the map
		nodesMutex.Lock()
		delete(nodes, nodeID)
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

		messageStr := string(message)
		logger.Printf("Received message from %s: %s", nodeID, messageStr)

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

// Simple HTML page for testing
func homeHandler(w http.ResponseWriter, r *http.Request) {
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
    </style>
</head>
<body>
    <h1>WebSocket Debug</h1>
    <div id="messages"></div>
    <div id="controls">
        <input type="text" id="messageInput" placeholder="Type a message...">
        <button onclick="sendMessage()">Send</button>
    </div>
    <div id="status">Connecting...</div>
    <div>
        <p>Connected clients: <span id="clientCount">0</span></p>
    </div>
</body>
</html>
	`)
}

// Main function
func main() {
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", homeHandler)

	logger.Printf("Starting debug server on port %d", serverPort)
	logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", serverPort), nil))
}
