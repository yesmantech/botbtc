//+------------------------------------------------------------------+
//| BingX_PriceFeeder.mq5                                            |
//| Receives real BTC price from Go server via TCP (zero HTTP)       |
//| and feeds it into MT5 as custom symbol "BTCUSDT.bingx"           |
//|                                                                   |
//| HOW IT WORKS:                                                     |
//|  1. Creates custom symbol "BTCUSDT.bingx" in MT5                 |
//|  2. Connects to Go server price feed on TCP port 9091            |
//|  3. Receives tick data pushed by the Go server (sub-ms latency)  |
//|  4. Injects ticks into custom symbol via CustomTicksAdd()        |
//|                                                                   |
//| WHY TCP INSTEAD OF HTTP:                                          |
//|  - WebRequest() in MT5 is BLOCKING (~15ms per call)              |
//|  - TCP socket read is non-blocking (<0.1ms via localhost)        |
//|  - Go server already has BingX prices from its market poller     |
//|  - No extra HTTP calls = zero impact on trading latency          |
//|                                                                   |
//| SETUP:                                                            |
//|  1. Start the Go server (it opens port 9091 for price feed)     |
//|  2. Attach this EA to any chart                                  |
//|  3. Open chart for "BTCUSDT.bingx" custom symbol                 |
//|  4. Attach BTC_Scalper_Signal EA to that chart                   |
//+------------------------------------------------------------------+
#property copyright "BotBTC"
#property version   "2.00"
#property strict

#include <Trade\SymbolInfo.mqh>

// ── Inputs ──────────────────────────────────────────────────────────
input string FeedHost        = "127.0.0.1";       // Go server host
input int    FeedPort        = 9091;               // Price feed port
input string CustomSymbol    = "BTCUSDT.bingx";   // Custom symbol name
input int    ReconnectMs     = 2000;               // Reconnect interval on disconnect

// ── Globals ─────────────────────────────────────────────────────────
int    g_socket = INVALID_HANDLE;
int    g_tickCount = 0;
int    g_errorCount = 0;
bool   g_connected = false;

//+------------------------------------------------------------------+
//| Expert initialization                                             |
//+------------------------------------------------------------------+
int OnInit()
{
   // Create custom symbol if it doesn't exist
   bool isCustom = false;
   if(!SymbolExist(CustomSymbol, isCustom))
   {
      if(!CustomSymbolCreate(CustomSymbol, "", "BingX"))
      {
         Print("❌ Failed to create custom symbol: ", CustomSymbol);
         return INIT_FAILED;
      }
      Print("✅ Custom symbol created: ", CustomSymbol);
   }
   else
   {
      Print("✅ Custom symbol already exists: ", CustomSymbol);
   }
   
   // Set symbol properties for BTC
   CustomSymbolSetInteger(CustomSymbol, SYMBOL_DIGITS, 1);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_POINT, 0.1);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_TRADE_TICK_SIZE, 0.1);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_TRADE_TICK_VALUE, 0.1);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_MIN, 0.001);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_MAX, 100.0);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_STEP, 0.001);
   CustomSymbolSetString(CustomSymbol, SYMBOL_CURRENCY_BASE, "BTC");
   CustomSymbolSetString(CustomSymbol, SYMBOL_CURRENCY_PROFIT, "USDT");
   CustomSymbolSetString(CustomSymbol, SYMBOL_DESCRIPTION, "BTC/USDT from BingX (via Go server)");
   
   // Enable in Market Watch
   SymbolSelect(CustomSymbol, true);
   
   // Connect to Go server price feed
   ConnectToFeed();
   
   // Poll every 50ms for incoming price data
   EventSetMillisecondTimer(50);
   
   Print("🚀 BingX Price Feeder v2 started (TCP mode, zero HTTP)");
   return INIT_SUCCEEDED;
}

//+------------------------------------------------------------------+
//| Expert deinitialization                                           |
//+------------------------------------------------------------------+
void OnDeinit(const int reason)
{
   EventKillTimer();
   if(g_socket != INVALID_HANDLE)
   {
      SocketClose(g_socket);
      g_socket = INVALID_HANDLE;
   }
   Print("🛑 Price Feeder stopped. Ticks: ", g_tickCount, " Errors: ", g_errorCount);
}

//+------------------------------------------------------------------+
//| Connect to Go server price feed                                  |
//+------------------------------------------------------------------+
bool ConnectToFeed()
{
   if(g_socket != INVALID_HANDLE)
   {
      SocketClose(g_socket);
      g_socket = INVALID_HANDLE;
   }
   
   g_socket = SocketCreate();
   if(g_socket == INVALID_HANDLE)
   {
      Print("❌ SocketCreate failed: ", GetLastError());
      g_connected = false;
      return false;
   }
   
   if(!SocketConnect(g_socket, FeedHost, FeedPort, 3000))
   {
      Print("⚠️ Cannot connect to ", FeedHost, ":", FeedPort, " — retrying...");
      SocketClose(g_socket);
      g_socket = INVALID_HANDLE;
      g_connected = false;
      return false;
   }
   
   g_connected = true;
   Print("✅ Connected to price feed ", FeedHost, ":", FeedPort);
   return true;
}

//+------------------------------------------------------------------+
//| Timer — read price data from TCP socket                          |
//+------------------------------------------------------------------+
void OnTimer()
{
   // Reconnect if disconnected
   if(!g_connected || g_socket == INVALID_HANDLE)
   {
      ConnectToFeed();
      return;
   }
   
   // Check if data is available (non-blocking)
   uint dataLen = SocketIsReadable(g_socket);
   if(dataLen == 0)
      return;
   
   // Read data
   uchar buf[];
   int bytesRead = SocketRead(g_socket, buf, dataLen, 10);
   if(bytesRead <= 0)
   {
      g_errorCount++;
      g_connected = false;
      Print("⚠️ Socket read error, reconnecting...");
      return;
   }
   
   // Parse received data — format: "BID ASK\n" per line
   // Example: "77232.4 77232.6\n"
   string data = CharArrayToString(buf, 0, bytesRead, CP_UTF8);
   
   // Split by newlines in case multiple ticks arrived
   string lines[];
   int lineCount = StringSplit(data, '\n', lines);
   
   for(int i = 0; i < lineCount; i++)
   {
      string line = lines[i];
      StringTrimRight(line);
      StringTrimLeft(line);
      if(StringLen(line) == 0) continue;
      
      // Parse "BID ASK"
      string parts[];
      int partCount = StringSplit(line, ' ', parts);
      if(partCount < 2) continue;
      
      double bidPrice = StringToDouble(parts[0]);
      double askPrice = StringToDouble(parts[1]);
      
      if(bidPrice <= 0 || askPrice <= 0) continue;
      
      // Inject tick
      InjectTick(bidPrice, askPrice);
   }
}

//+------------------------------------------------------------------+
//| Inject a tick into the custom symbol                             |
//+------------------------------------------------------------------+
void InjectTick(double bid, double ask)
{
   MqlTick tick;
   ZeroMemory(tick);
   tick.time      = TimeCurrent();
   tick.time_msc  = (long)GetMicrosecondCount() / 1000;
   tick.bid       = bid;
   tick.ask       = ask;
   tick.last      = (bid + ask) / 2.0;
   tick.volume    = 1;
   tick.flags     = TICK_FLAG_BID | TICK_FLAG_ASK | TICK_FLAG_LAST;
   
   MqlTick ticks[];
   ArrayResize(ticks, 1);
   ticks[0] = tick;
   
   int added = CustomTicksAdd(CustomSymbol, ticks);
   if(added > 0)
   {
      g_tickCount++;
      if(g_tickCount % 500 == 0)
         Print("📊 Tick #", g_tickCount, " — bid=", bid, " ask=", ask);
   }
}

//+------------------------------------------------------------------+
//| OnTick — not used                                                |
//+------------------------------------------------------------------+
void OnTick() {}
//+------------------------------------------------------------------+
