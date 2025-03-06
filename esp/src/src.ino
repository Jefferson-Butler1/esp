#include <Arduino.h>
#include <ESP8266WiFi.h>
#include <WebSocketsClient.h>

// WiFi network configuration
const char* WIFI_SSID = "Harold";
const char* WIFI_PASSWORD = "gigantic";

// Mac server configuration
const char* MAC_SERVER_IP = "192.168.0.100";
const int MAC_SERVER_PORT = 3200;

WebSocketsClient webSocket;
unsigned long lastPingTime = 0;
const int pingInterval = 5000; // Send a ping every 5 seconds

void webSocketEvent(WStype_t type, uint8_t * payload, size_t length) {
  switch (type) {
    case WStype_DISCONNECTED:
      Serial.println("WebSocket disconnected!");
      break;
    case WStype_CONNECTED:
      Serial.println("WebSocket connected!");
      // Wait a moment before sending first message
      delay(500);
      // Send a simple message after connection
      Serial.println("Sending REGISTER message...");
      webSocket.sendTXT("REGISTER");
      break;
    case WStype_TEXT:
      Serial.print("Received text: ");
      for(size_t i=0; i < length; i++) {
        Serial.print((char)payload[i]);
      }
      Serial.println();
      break;
    case WStype_ERROR:
      Serial.println("WebSocket error!");
      break;
    case WStype_BIN:
      Serial.println("Received binary data");
      break;
    default:
      // Handle other cases
      Serial.print("WebSocket event type: ");
      Serial.println(type);
      break;
  }
}

void setup() {
  Serial.begin(115200);
  delay(100);
  
  Serial.println("\n\n=== ESP8266 WebSocket Debug ===");
  
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
  
  // Connect to the Mac server via WebSocket
  Serial.print("Connecting to WebSocket server at ");
  Serial.print(MAC_SERVER_IP);
  Serial.print(":");
  Serial.println(MAC_SERVER_PORT);
  
  // Connect to WebSocket server (non-SSL)
  webSocket.begin(MAC_SERVER_IP, MAC_SERVER_PORT, "/ws");
  webSocket.onEvent(webSocketEvent);
  webSocket.setReconnectInterval(5000);
  
  // Set lower packet size to prevent buffer issues
  webSocket.setExtraHeaders(""); // No extra headers
  Serial.println("WebSocket configuration complete");
}

void loop() {
  webSocket.loop();
  
  // Send a simple ping message periodically
  unsigned long currentTime = millis();
  if (currentTime - lastPingTime >= pingInterval) {
    lastPingTime = currentTime;
    Serial.println("Sending ping...");
    webSocket.sendTXT("PING");
  }
}
