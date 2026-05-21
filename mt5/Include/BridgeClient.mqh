//+------------------------------------------------------------------+
//|                                                 BridgeClient.mqh |
//|                        BTC Scalper Signal Generator               |
//|                        TCP socket client for Python bridge        |
//+------------------------------------------------------------------+
#property copyright "BTC Scalper"
#property strict

#ifndef BRIDGE_CLIENT_MQH
#define BRIDGE_CLIENT_MQH

//+------------------------------------------------------------------+
//| CBridgeClient — TCP socket client using MQL5 Socket* functions   |
//+------------------------------------------------------------------+
class CBridgeClient
{
private:
   int               m_socket;               // socket handle (-1 = invalid)
   string            m_host;                 // bridge host
   int               m_port;                 // bridge port
   int               m_timeout;              // connect / send timeout (ms)
   bool              m_connected;            // logical connection state
   datetime          m_lastConnectAttempt;   // for reconnect throttling
   int               m_reconnectInterval;    // seconds between reconnect tries

   // --- Stats ---
   int               m_messagesSent;         // total messages sent
   int               m_messagesReceived;     // total messages received
   long              m_connectedSinceMs;     // tick count when connection established

public:
   //--- Constructor / Destructor
                     CBridgeClient();
                    ~CBridgeClient();

   //--- Connection management
   bool              Connect(const string host, const int port, const int timeout);
   void              Disconnect();
   bool              IsConnected()           { return m_connected; }

   //--- I/O
   bool              Send(const string json_payload);
   string            Receive(const int timeout_ms);

   //--- Reconnection helper
   bool              TryReconnect();

   //--- High-level: send a signal and wait for ACK
   bool              SendSignal(const string json_payload);

   //--- Configure reconnect interval
   void              SetReconnectInterval(const int seconds) { m_reconnectInterval = seconds; }

   //--- Connection stats ---
   int               GetMessagesSent()       { return m_messagesSent;     }
   int               GetMessagesReceived()   { return m_messagesReceived; }
   long              GetUptimeMs();
};

//+------------------------------------------------------------------+
//| Constructor                                                       |
//+------------------------------------------------------------------+
CBridgeClient::CBridgeClient()
   : m_socket(-1),
     m_host("127.0.0.1"),
     m_port(9090),
     m_timeout(3000),
     m_connected(false),
     m_lastConnectAttempt(0),
     m_reconnectInterval(5),
     m_messagesSent(0),
     m_messagesReceived(0),
     m_connectedSinceMs(0)
{
}

//+------------------------------------------------------------------+
//| Destructor — ensure socket is released                           |
//+------------------------------------------------------------------+
CBridgeClient::~CBridgeClient()
{
   Disconnect();
}

//+------------------------------------------------------------------+
//| GetUptimeMs — how long the current connection has been alive     |
//+------------------------------------------------------------------+
long CBridgeClient::GetUptimeMs()
{
   if(!m_connected || m_connectedSinceMs == 0)
      return 0;
   return (long)GetTickCount64() - m_connectedSinceMs;
}

//+------------------------------------------------------------------+
//| Connect — create socket and connect to bridge                    |
//+------------------------------------------------------------------+
bool CBridgeClient::Connect(const string host, const int port, const int timeout)
{
   // Clean up any previous socket
   Disconnect();

   m_host    = host;
   m_port    = port;
   m_timeout = timeout;

   m_lastConnectAttempt = TimeCurrent();

   PrintFormat("[BridgeClient] Connecting to %s:%d (timeout %d ms) …", m_host, m_port, m_timeout);

   // --- Create the socket ------------------------------------------------
   m_socket = SocketCreate();
   if(m_socket == INVALID_HANDLE)
   {
      int err = GetLastError();
      PrintFormat("[BridgeClient] SocketCreate failed – error %d.  "
                  "Check Tools ▸ Options ▸ Expert Advisors ▸ "
                  "\"Allow WebRequest / Socket connections\".", err);
      return false;
   }

   // --- Connect to bridge ------------------------------------------------
   if(!SocketConnect(m_socket, m_host, m_port, m_timeout))
   {
      int err = GetLastError();
      PrintFormat("[BridgeClient] SocketConnect(%s:%d) failed – error %d",
                  m_host, m_port, err);
      SocketClose(m_socket);
      m_socket    = -1;
      m_connected = false;
      return false;
   }

   m_connected       = true;
   m_connectedSinceMs = (long)GetTickCount64();
   PrintFormat("[BridgeClient] Connected to %s:%d  (socket handle %d)",
              m_host, m_port, m_socket);
   return true;
}

//+------------------------------------------------------------------+
//| Disconnect — close socket and reset state                        |
//+------------------------------------------------------------------+
void CBridgeClient::Disconnect()
{
   if(m_socket != -1 && m_socket != INVALID_HANDLE)
   {
      SocketClose(m_socket);
      long uptime = GetUptimeMs();
      PrintFormat("[BridgeClient] Disconnected (socket %d)  uptime=%lld ms  sent=%d  recv=%d",
                  m_socket, uptime, m_messagesSent, m_messagesReceived);
   }
   m_socket          = -1;
   m_connected       = false;
   m_connectedSinceMs = 0;
}

//+------------------------------------------------------------------+
//| Send — transmit a newline-delimited JSON string                  |
//+------------------------------------------------------------------+
bool CBridgeClient::Send(const string json_payload)
{
   if(!m_connected || m_socket == -1)
   {
      Print("[BridgeClient] Send failed – not connected");
      return false;
   }

   // Append newline delimiter
   string data = json_payload + "\n";

   // Convert string → uchar array (UTF-8)
   uchar send_buffer[];
   int   len = StringToCharArray(data, send_buffer, 0, WHOLE_ARRAY, CP_UTF8);
   if(len <= 0)
   {
      Print("[BridgeClient] StringToCharArray failed – payload may be empty or invalid");
      return false;
   }

   // StringToCharArray appends a null terminator — exclude it
   int bytes_to_send = len - 1;
   if(bytes_to_send <= 0)
   {
      Print("[BridgeClient] Nothing to send after trimming null terminator");
      return false;
   }

   int sent = SocketSend(m_socket, send_buffer, bytes_to_send);
   if(sent == -1 || sent != bytes_to_send)
   {
      int err = GetLastError();
      PrintFormat("[BridgeClient] SocketSend failed – error %d (sent %d / %d)",
                  err, sent, bytes_to_send);
      m_connected = false;
      return false;
   }

   m_messagesSent++;
   return true;
}

//+------------------------------------------------------------------+
//| Receive — read from socket until newline or timeout              |
//+------------------------------------------------------------------+
string CBridgeClient::Receive(const int timeout_ms)
{
   if(!m_connected || m_socket == -1)
      return "";

   uchar recv_buffer[];
   string result = "";

   // SocketRead blocks up to timeout_ms; bridge sends newline-delimited JSON
   uint start_tick = GetTickCount();
   while(GetTickCount() - start_tick < (uint)timeout_ms)
   {
      uchar buf[];
      int received = SocketRead(m_socket, buf, 1024, 100); // 100 ms micro-wait
      if(received > 0)
      {
         string chunk = CharArrayToString(buf, 0, received, CP_UTF8);
         result += chunk;
         // Check for newline delimiter
         if(StringFind(result, "\n") >= 0)
         {
            // Trim trailing newline/whitespace
            StringTrimRight(result);
            m_messagesReceived++;
            return result;
         }
      }
      else if(received == 0)
      {
         // No data yet — keep waiting
         continue;
      }
      else
      {
         // Error
         int err = GetLastError();
         if(err != 0)
         {
            PrintFormat("[BridgeClient] SocketRead error %d", err);
            m_connected = false;
            return "";
         }
      }
   }

   // Timeout reached — return whatever we have
   StringTrimRight(result);
   if(StringLen(result) > 0)
      m_messagesReceived++;
   return result;
}

//+------------------------------------------------------------------+
//| TryReconnect — throttled reconnection attempt                    |
//+------------------------------------------------------------------+
bool CBridgeClient::TryReconnect()
{
   if(m_connected)
      return true;

   datetime now = TimeCurrent();
   if(now - m_lastConnectAttempt < m_reconnectInterval)
      return false;  // throttled — too soon

   PrintFormat("[BridgeClient] Attempting reconnect to %s:%d …", m_host, m_port);
   return Connect(m_host, m_port, m_timeout);
}

//+------------------------------------------------------------------+
//| SendSignal — send JSON and wait for ACK from bridge              |
//+------------------------------------------------------------------+
bool CBridgeClient::SendSignal(const string json_payload)
{
   if(!Send(json_payload))
      return false;

   // Wait up to 1 second for ACK
   string ack = Receive(1000);
   if(StringLen(ack) == 0)
   {
      Print("[BridgeClient] No ACK received within timeout");
      return false;
   }

   // Simple ACK check — bridge should respond with something containing "ok"
   if(StringFind(ack, "ok") >= 0 || StringFind(ack, "OK") >= 0 ||
      StringFind(ack, "ack") >= 0 || StringFind(ack, "ACK") >= 0)
   {
      return true;
   }

   PrintFormat("[BridgeClient] Unexpected ACK response: %s", ack);
   return true; // still count as sent even if ACK format is unexpected
}

#endif // BRIDGE_CLIENT_MQH
//+------------------------------------------------------------------+
