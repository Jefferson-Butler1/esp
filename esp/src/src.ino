#include <Arduino.h>
#include <ESP8266WiFi.h>
#include <WebSocketsClient.h>
#include <ArduinoJson.h>

// WiFi network configuration
const char* WIFI_SSID = "Harold";
const char* WIFI_PASSWORD = "gigantic";

// Mac server configuration
const char* MAC_SERVER_IP = "192.168.0.100";
const int MAC_SERVER_PORT = 3200;

// For RSSI measurements between nodes
const int RSSI_SAMPLES = 5;
const int SCAN_INTERVAL = 1000;

WebSocketsClient webSocket;
unsigned long lastPingTime = 0;
unsigned long lastScanTime = 0;
const int pingInterval = 5000;

String nodeID = ""; // Will be set after connecting to server
String macAddress = "";

// Map to store RSSI measurements between this node and other nodes
struct NodeRSSI {
  String nodeID;
  int rssi;
  unsigned long lastUpdate;
};

#define MAX_NODES 10
NodeRSSI knownNodes[MAX_NODES];
int nodeCount = 0;

void webSocketEvent(WStype_t type, uint8_t * payload, size_t length) {
  switch (type) {
    case WStype_DISCONNECTED:
      Serial.println("WebSocket disconnected!");
      break;
      
    case WStype_CONNECTED:
      Serial.println("WebSocket connected!");
      delay(500);
      
      // Register with server
      Serial.println("Sending REGISTER message...");
      webSocket.sendTXT("REGISTER");
      
      // Also send our MAC address for node identification
      String macMsg = "MAC:" + macAddress;
      webSocket.sendTXT(macMsg);
      break;
      
    case WStype_TEXT: {
      Serial.print("Received text: ");
      String message = "";
      for(size_t i=0; i < length; i++) {
        message += (char)payload[i];
      }
      Serial.println(message);
      
      // Check if this is an ID assignment message
      if (message.startsWith("ID:")) {
        nodeID = message.substring(3);
        Serial.print("Assigned node ID: ");
        Serial.println(nodeID);
        
        // Start auto-calibration process
        requestCalibration();
      }
      // Check if we received a calibration command
      else if (message.startsWith("CALIBRATE")) {
        startCalibration();
      }
      break;
    }
    
    case WStype_ERROR:
      Serial.println("WebSocket error!");
      break;
      
    default:
      Serial.print("WebSocket event type: ");
      Serial.println(type);
      break;
  }
}

// Request calibration from the server
void requestCalibration() {
  if (nodeID.length() == 0) {
    Serial.println("Cannot request calibration: Node ID not yet assigned");
    return;
  }
  
  Serial.println("Requesting auto-calibration from server...");
  
  // Create JSON document for calibration request
  JsonDocument doc;
  doc["type"] = "calibration_request";
  doc["node_id"] = nodeID;
  doc["mac"] = macAddress;
  
  // Serialize to string
  String jsonStr;
  serializeJson(doc, jsonStr);
  
  // Send to server
  webSocket.sendTXT(jsonStr);
}

// Start calibration process - scan for other ESP nodes
void startCalibration() {
  Serial.println("Starting auto-calibration process...");
  Serial.println("Scanning for other ESP nodes...");
  
  // Perform WiFi scan to find other nodes
  scanForOtherNodes();
  
  // Send results to server
  reportCalibrationResults();
}

// Scan for other ESP nodes by looking for their WiFi signals
void scanForOtherNodes() {
  int numNetworks = WiFi.scanNetworks();
  
  if (numNetworks == 0) {
    Serial.println("No networks found");
    return;
  }
  
  Serial.printf("Found %d networks\n", numNetworks);
  
  // Look for ESP nodes - they usually have MAC addresses starting with specific prefixes
  // Common ESP8266 MAC prefixes: 5C:CF:7F, 18:FE:34, 24:0A:C4, etc.
  for (int i = 0; i < numNetworks; i++) {
    String ssid = WiFi.SSID(i);
    String bssid = WiFi.BSSIDstr(i);
    int rssi = WiFi.RSSI(i);
    
    Serial.printf("%d: %s (%s) %d dBm\n", i + 1, ssid.c_str(), bssid.c_str(), rssi);
    
    // Check if this is likely an ESP8266
    // In a real implementation, you'd want a more reliable way to identify your ESP nodes
    // such as having them broadcast a specific SSID prefix
    if (isESPMacAddress(bssid)) {
      // Store or update RSSI for this node
      updateNodeRSSI(bssid, rssi);
    }
  }
  
  // Clean up scan resources
  WiFi.scanDelete();
}

// Simple check if a MAC address likely belongs to an ESP8266
// This is just a basic implementation - you might need to customize this
bool isESPMacAddress(String mac) {
  // Common ESP8266 MAC address prefixes
  // These are just examples, your devices may have different prefixes
  const char* espPrefixes[] = {"5C:CF:7F", "18:FE:34", "24:0A:C4", "60:01:94", "A4:CF:12"};
  const int numPrefixes = 5;
  
  for (int i = 0; i < numPrefixes; i++) {
    if (mac.startsWith(espPrefixes[i])) {
      return true;
    }
  }
  
  // Alternative check: exclude known non-ESP devices
  if (mac.startsWith("00:00:00") || mac.equals(WiFi.macAddress())) {
    return false;
  }
  
  // For testing, consider all other devices as potential ESPs
  // In a real implementation, you would want a more accurate way to identify your nodes
  return true;
}

// Update or add a node's RSSI measurement
void updateNodeRSSI(String mac, int rssi) {
  // First check if we already know this node
  for (int i = 0; i < nodeCount; i++) {
    if (knownNodes[i].nodeID == mac) {
      // Update existing entry
      knownNodes[i].rssi = rssi;
      knownNodes[i].lastUpdate = millis();
      Serial.printf("Updated RSSI for node %s: %d dBm\n", mac.c_str(), rssi);
      return;
    }
  }
  
  // If we get here, this is a new node
  if (nodeCount < MAX_NODES) {
    knownNodes[nodeCount].nodeID = mac;
    knownNodes[nodeCount].rssi = rssi;
    knownNodes[nodeCount].lastUpdate = millis();
    nodeCount++;
    Serial.printf("Added new node %s with RSSI: %d dBm\n", mac.c_str(), rssi);
  } else {
    Serial.println("WARNING: Too many nodes, cannot store more");
  }
}

// Report calibration results to the server
void reportCalibrationResults() {
  if (nodeCount == 0) {
    Serial.println("No other nodes found for calibration");
    return;
  }
  
  Serial.printf("Reporting calibration results for %d nodes\n", nodeCount);
  
  // Create JSON document
  JsonDocument doc;
  doc["type"] = "calibration_data";
  doc["node_id"] = nodeID;
  doc["mac"] = macAddress;
  
  JsonArray nodesArray = doc.createNestedArray("nodes");
  
  for (int i = 0; i < nodeCount; i++) {
    JsonObject nodeObj = nodesArray.add<JsonObject>();
    nodeObj["mac"] = knownNodes[i].nodeID;
    nodeObj["rssi"] = knownNodes[i].rssi];
  }
  
  // Serialize to string
  String jsonStr;
  serializeJson(doc, jsonStr);
  
  // Send to server
  Serial.println("Sending calibration data to server:");
  Serial.println(jsonStr);
  webSocket.sendTXT(jsonStr);
}

void setup() {
  Serial.begin(115200);
  delay(100);
  
  Serial.println("\n\n=== ESP8266 Auto-Calibration Node ===");
  
  // Get MAC address
  macAddress = WiFi.macAddress();
  Serial.print("MAC Address: ");
  Serial.println(macAddress);
  
  // Connect to WiFi
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  
  Serial.print("Connecting to WiFi");
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  
  Serial.println("\nWiFi connected");
  Serial.print("IP address: ");
  Serial.println(WiFi.localIP());
  
  // Connect to the server via WebSocket
  Serial.printf("Connecting to WebSocket server at %s:%d\n", 
                MAC_SERVER_IP, MAC_SERVER_PORT);
  
  webSocket.begin(MAC_SERVER_IP, MAC_SERVER_PORT, "/ws");
  webSocket.onEvent(webSocketEvent);
  webSocket.setReconnectInterval(5000);
  webSocket.setExtraHeaders("");
  
  Serial.println("WebSocket configuration complete");
}

void loop() {
  webSocket.loop();
  
  // Send a ping periodically
  unsigned long currentTime = millis();
  if (currentTime - lastPingTime >= pingInterval) {
    lastPingTime = currentTime;
    webSocket.sendTXT("PING");
  }
  
  // Periodically re-scan for other nodes during calibration phase
  if (currentTime - lastScanTime >= SCAN_INTERVAL) {
    lastScanTime = currentTime;
    
    // Only scan if we're in active calibration or if requested by server
    // (This is just a placeholder - you would implement a proper state machine)
    // scanForOtherNodes();
  }
}
