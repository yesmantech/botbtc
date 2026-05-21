//+------------------------------------------------------------------+
//| BingX_PriceFeeder.mq5 — v3 (deferred symbol creation)           |
//+------------------------------------------------------------------+
#property copyright "BotBTC"
#property version   "3.00"
#property strict

// ── Inputs ──────────────────────────────────────────────────────────
input string FeedHost        = "127.0.0.1";
input int    FeedPort        = 9091;
input string CustomSymbol    = "BTCUSDT.bingx";
input int    ReconnectMs     = 2000;

// ── Globals ─────────────────────────────────────────────────────────
int    g_socket = INVALID_HANDLE;
int    g_tickCount = 0;
int    g_errorCount = 0;
bool   g_connected = false;
bool   g_symbolReady = false;  // deferred init flag

//+------------------------------------------------------------------+
int OnInit()
{
   // Do NOT touch symbols here — just start the timer
   EventSetMillisecondTimer(100);
   Print("🚀 BingX Price Feeder v3 started (deferred init)");
   return INIT_SUCCEEDED;
}

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
bool SetupCustomSymbol()
{
   if(!TerminalInfoInteger(TERMINAL_CONNECTED))
   {
      Print("⏳ Waiting for terminal connection...");
      return false;
   }
   
   bool isCustom = false;
   if(!SymbolExist(CustomSymbol, isCustom))
   {
      if(!CustomSymbolCreate(CustomSymbol, "", "BingX"))
      {
         Print("❌ Failed to create custom symbol: ", CustomSymbol, " err=", GetLastError());
         return false;
      }
      Print("✅ Custom symbol created: ", CustomSymbol);
   }
   else
   {
      Print("✅ Custom symbol already exists: ", CustomSymbol);
   }
   
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
   SymbolSelect(CustomSymbol, true);
   
   return true;
}

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
      g_connected = false;
      return false;
   }
   
   if(!SocketConnect(g_socket, FeedHost, FeedPort, 3000))
   {
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
void OnTimer()
{
   // Phase 1: deferred symbol setup
   if(!g_symbolReady)
   {
      g_symbolReady = SetupCustomSymbol();
      if(!g_symbolReady) return;
      ConnectToFeed();
      EventSetMillisecondTimer(50);  // speed up after init
      return;
   }
   
   // Phase 2: reconnect if needed
   if(!g_connected || g_socket == INVALID_HANDLE)
   {
      ConnectToFeed();
      return;
   }
   
   // Phase 3: read price data
   uint dataLen = SocketIsReadable(g_socket);
   if(dataLen == 0) return;
   
   uchar buf[];
   int bytesRead = SocketRead(g_socket, buf, dataLen, 10);
   if(bytesRead <= 0)
   {
      g_errorCount++;
      g_connected = false;
      return;
   }
   
   string data = CharArrayToString(buf, 0, bytesRead, CP_UTF8);
   string lines[];
   int lineCount = StringSplit(data, '\n', lines);
   
   for(int i = 0; i < lineCount; i++)
   {
      string line = lines[i];
      StringTrimRight(line);
      StringTrimLeft(line);
      if(StringLen(line) == 0) continue;
      
      string parts[];
      int partCount = StringSplit(line, ' ', parts);
      if(partCount < 2) continue;
      
      double bidPrice = StringToDouble(parts[0]);
      double askPrice = StringToDouble(parts[1]);
      if(bidPrice <= 0 || askPrice <= 0) continue;
      
      InjectTick(bidPrice, askPrice);
   }
}

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
void OnTick() {}
//+------------------------------------------------------------------+
