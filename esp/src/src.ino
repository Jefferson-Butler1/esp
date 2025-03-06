#include <ESP8266WiFi.h>
#include <ESP8266HTTPClient.h>
#include <WebSocketsClient.h>
#include <ArduinoJson.h>

// WiFi credentials for your main network
const char* ssid = "Harold";
const char* password = "gigantic";

// Server details
const char* serverIP = "192.168.0.100";
const int serverPort = 3200;

// Node variables
int nodeId = -1;                         // Will be assigned by server
String apSSID = "";                      // Will be "node-X" where X is nodeId
const int MAX_NODES = 10;                // Maximum number of nodes to track
String nodeSSIDs[MAX_NODES];             // Array to store other node SSIDs
int lastRSSI[MAX_NODES];                 // Array to store last RSSI values

// Timing variables
unsigned long lastScanTime = 0;
const int scanInterval = 100;            // 100ms interval for 10Hz frequency

// WebSocket client
WebSocketsClient webSocket;

void setup() {
  // Initialize serial communication
  Serial.begin(115200);
  Serial.println("\nESP8266 RSSI Mesh Node Starting");
  
  // Connect to WiFi network
  WiFi.mode(WIFI_AP_STA);                // Set to dual mode: Station + Access Point
  WiFi.begin(ssid, password);
  
  Serial.print("Connecting to WiFi");
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  Serial.println();
  Serial.print("Connected to WiFi, IP address: ");
  Serial.println(WiFi.localIP());
  
  // Connect to WebSocket server
  connectWebSocket();
}

void loop() {
  // Handle WebSocket events
  webSocket.loop();
  
  // If we have a nodeId assigned, perform scanning at the desired frequency
  if (nodeId >= 0) {
    unsigned long currentTime = millis();
    if (currentTime - lastScanTime >= scanInterval) {
      scanForNodes();
      sendRSSIData();
      lastScanTime = currentTime;
    }
  }
}

void connectWebSocket() {
  // WebSocket server setup
  webSocket.begin(serverIP, serverPort, "/ws");
  
  // WebSocket event handler
  webSocket.onEvent(webSocketEvent);
  
  // Set reconnect interval to 5s
  webSocket.setReconnectInterval(5000);
  
  Serial.println("WebSocket connection established");
}

void webSocketEvent(WStype_t type, uint8_t * payload, size_t length) {
  switch(type) {
    case WStype_DISCONNECTED:
      Serial.println("WebSocket disconnected");
      break;
      
    case WStype_CONNECTED:
      Serial.println("WebSocket connected");
      // Send registration message
      sendRegistration();
      break;
      
    case WStype_TEXT:
      handleWebSocketMessage(payload, length);
      break;
    default:
      Serial.println("New Event Type:");
      break;
  }
}

void sendRegistration() {
  // Create JSON message for registration
  DynamicJsonDocument doc(256);
  doc["type"] = "register";
  doc["mac"] = WiFi.macAddress();
  
  // Serialize JSON to string
  String message;
  serializeJson(doc, message);
  
  // Send registration message
  webSocket.sendTXT(message);
  Serial.println("Registration sent: " + message);
}

void handleWebSocketMessage(uint8_t * payload, size_t length) {
  // Parse JSON message
  DynamicJsonDocument doc(1024);
  DeserializationError error = deserializeJson(doc, payload, length);
  
  if (error) {
    Serial.print("JSON parsing failed: ");
    Serial.println(error.c_str());
    return;
  }
  
  // Check message type
  String type = doc["type"];
  
  if (type == "id_assignment") {
    // Handle node ID assignment
    nodeId = doc["id"];
    apSSID = "node-" + String(nodeId);
    
    // Set up Access Point with the assigned ID
    setupAP();
    
    Serial.print("Assigned node ID: ");
    Serial.println(nodeId);
  }
  else if (type == "node_list") {
    // Handle list of active nodes
    JsonArray nodes = doc["nodes"];
    int nodeCount = 0;
    
    // Clear existing list first
    for (int i = 0; i < MAX_NODES; i++) {
      nodeSSIDs[i] = "";
    }
    
    // Update node list
    for (JsonVariant node : nodes) {
      int id = node["id"];
      if (id != nodeId && nodeCount < MAX_NODES) {  // Skip our own node
        nodeSSIDs[nodeCount] = "node-" + String(id);
        nodeCount++;
      }
    }
    
    Serial.print("Updated node list. Tracking ");
    Serial.print(nodeCount);
    Serial.println(" other nodes.");
  }
}

void setupAP() {
  // Setup access point with the assigned node ID
  bool success = WiFi.softAP(apSSID.c_str(), "");
  
  if (success) {
    Serial.print("Access Point created with SSID: ");
    Serial.println(apSSID);
    Serial.print("AP IP address: ");
    Serial.println(WiFi.softAPIP());
  } else {
    Serial.println("Failed to create Access Point");
  }
}

void scanForNodes() {
  Serial.println("Scanning for node APs...");
  
  // Start WiFi scan
  int networks = WiFi.scanNetworks();
  
  // Reset RSSI values to minimum
  for (int i = 0; i < MAX_NODES; i++) {
    if (nodeSSIDs[i] != "") {
      lastRSSI[i] = -100;  // Default to very weak signal if not found
    }
  }
  
  if (networks > 0) {
    // Go through all networks found
    for (int i = 0; i < networks; i++) {
      String currentSSID = WiFi.SSID(i);
      int rssi = WiFi.RSSI(i);
      
      // Check if this network is one of our nodes
      for (int j = 0; j < MAX_NODES; j++) {
        if (nodeSSIDs[j] != "" && currentSSID == nodeSSIDs[j]) {
          lastRSSI[j] = rssi;
          Serial.print("Found node: ");
          Serial.print(nodeSSIDs[j]);
          Serial.print(" with RSSI: ");
          Serial.println(rssi);
        }
      }
    }
  }
  
  // Free memory used by the scan
  WiFi.scanDelete();
}

void sendRSSIData() {
  // Only send if we have valid data
  if (nodeId < 0) return;
  
  // Create JSON message with RSSI values
  DynamicJsonDocument doc(1024);
  doc["type"] = "rssi_report";
  doc["node_id"] = nodeId;
  doc["timestamp"] = millis();
  
  JsonArray measurements = doc.createNestedArray("measurements");
  
  for (int i = 0; i < MAX_NODES; i++) {
    if (nodeSSIDs[i] != "") {
      JsonObject measurement = measurements.createNestedObject();
      
      // Extract nodeId from SSID (format: "node-X")
      String targetNodeId = nodeSSIDs[i].substring(5);
      
      measurement["target_id"] = targetNodeId.toInt();
      measurement["rssi"] = lastRSSI[i];
    }
  }
  
  // Serialize JSON to string
  String message;
  serializeJson(doc, message);
  
  // Send RSSI data
  webSocket.sendTXT(message);
}
