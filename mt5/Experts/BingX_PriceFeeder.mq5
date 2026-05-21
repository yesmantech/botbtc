//+------------------------------------------------------------------+
//| BingX_PriceFeeder.mq5                                            |
//| Fetches real BTC price from BingX API and feeds it into MT5      |
//| as a custom symbol "BTCUSDT.bingx"                                |
//|                                                                   |
//| HOW IT WORKS:                                                     |
//|  1. Creates a custom symbol "BTCUSDT.bingx" in MT5               |
//|  2. Polls BingX public API every 200ms for best bid/ask          |
//|  3. Injects ticks into the custom symbol via CustomTicksAdd()    |
//|  4. The scalper EA runs on a chart of this custom symbol         |
//|                                                                   |
//| SETUP:                                                            |
//|  1. Attach this EA to any chart (e.g. EURUSD M1)                |
//|  2. Allow WebRequest in MT5: Tools > Options > Expert Advisors   |
//|     Add URL: https://open-api.bingx.com                          |
//|  3. Open a chart for "BTCUSDT.bingx" custom symbol               |
//|  4. Attach BTC_Scalper_Signal EA to that chart                   |
//+------------------------------------------------------------------+
#property copyright "BotBTC"
#property version   "1.00"
#property strict

// ── Inputs ──────────────────────────────────────────────────────────
input int    PollIntervalMs  = 200;     // Poll interval (milliseconds)
input string CustomSymbol    = "BTCUSDT.bingx";  // Custom symbol name
input string BingxBaseUrl    = "https://open-api.bingx.com";

// ── Globals ─────────────────────────────────────────────────────────
string g_apiUrl;
bool   g_symbolCreated = false;
int    g_tickCount = 0;
int    g_errorCount = 0;

//+------------------------------------------------------------------+
//| Expert initialization                                             |
//+------------------------------------------------------------------+
int OnInit()
{
   // Build API URL
   g_apiUrl = BingxBaseUrl + "/openApi/swap/v2/quote/bookTicker?symbol=BTC-USDT";
   
   // Create custom symbol if it doesn't exist
   if(!SymbolExist(CustomSymbol, g_symbolCreated))
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
      Print("✅ Custom symbol exists: ", CustomSymbol);
   }
   
   // Set symbol properties
   CustomSymbolSetInteger(CustomSymbol, SYMBOL_DIGITS, 1);           // 1 decimal (77232.4)
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_POINT, 0.1);           // min price step
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_TRADE_TICK_SIZE, 0.1); // tick size
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_TRADE_TICK_VALUE, 0.1);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_MIN, 0.001);    // min lot
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_MAX, 100.0);
   CustomSymbolSetDouble(CustomSymbol, SYMBOL_VOLUME_STEP, 0.001);
   CustomSymbolSetString(CustomSymbol, SYMBOL_CURRENCY_BASE, "BTC");
   CustomSymbolSetString(CustomSymbol, SYMBOL_CURRENCY_PROFIT, "USDT");
   CustomSymbolSetString(CustomSymbol, SYMBOL_DESCRIPTION, "BTC/USDT from BingX Exchange");
   
   // Enable the symbol in Market Watch
   SymbolSelect(CustomSymbol, true);
   
   // Set timer for polling
   EventSetMillisecondTimer(PollIntervalMs);
   
   Print("🚀 BingX Price Feeder started — polling every ", PollIntervalMs, "ms");
   Print("📡 API URL: ", g_apiUrl);
   
   // Do initial fetch
   FetchAndInjectTick();
   
   return INIT_SUCCEEDED;
}

//+------------------------------------------------------------------+
//| Expert deinitialization                                           |
//+------------------------------------------------------------------+
void OnDeinit(const int reason)
{
   EventKillTimer();
   Print("🛑 BingX Price Feeder stopped. Total ticks: ", g_tickCount, " Errors: ", g_errorCount);
}

//+------------------------------------------------------------------+
//| Timer event — fires every PollIntervalMs                         |
//+------------------------------------------------------------------+
void OnTimer()
{
   FetchAndInjectTick();
}

//+------------------------------------------------------------------+
//| Fetch BingX price and inject into custom symbol                  |
//+------------------------------------------------------------------+
void FetchAndInjectTick()
{
   // ── HTTP request to BingX public API ──
   string headers = "";
   char   postData[];
   char   result[];
   string resultHeaders;
   
   int httpCode = WebRequest(
      "GET",                    // method
      g_apiUrl,                 // URL
      headers,                  // headers
      3000,                     // timeout ms
      postData,                 // POST data (empty for GET)
      result,                   // response body
      resultHeaders             // response headers
   );
   
   if(httpCode != 200)
   {
      g_errorCount++;
      if(g_errorCount % 50 == 1) // Log every 50th error to avoid spam
         Print("⚠️ BingX API error, HTTP ", httpCode, " (error #", g_errorCount, ")");
      return;
   }
   
   // ── Parse JSON response ──
   string json = CharArrayToString(result, 0, WHOLE_ARRAY, CP_UTF8);
   
   // Extract bid_price and ask_price from JSON
   double bidPrice = ExtractJsonDouble(json, "bid_price");
   double askPrice = ExtractJsonDouble(json, "ask_price");
   
   if(bidPrice <= 0 || askPrice <= 0)
   {
      g_errorCount++;
      if(g_errorCount % 50 == 1)
         Print("⚠️ Invalid price: bid=", bidPrice, " ask=", askPrice);
      return;
   }
   
   // ── Create tick and inject ──
   MqlTick tick;
   ZeroMemory(tick);
   tick.time      = TimeCurrent();
   tick.time_msc  = (long)(TimeLocal()) * 1000 + GetTickCount() % 1000;
   tick.bid       = bidPrice;
   tick.ask       = askPrice;
   tick.last      = (bidPrice + askPrice) / 2.0;
   tick.volume    = 1;
   tick.flags     = TICK_FLAG_BID | TICK_FLAG_ASK | TICK_FLAG_LAST;
   
   // Inject tick into custom symbol
   MqlTick ticks[];
   ArrayResize(ticks, 1);
   ticks[0] = tick;
   
   int added = CustomTicksAdd(CustomSymbol, ticks);
   if(added > 0)
   {
      g_tickCount++;
      // Log every 500 ticks (~100 seconds at 200ms interval)
      if(g_tickCount % 500 == 0)
         Print("📊 Tick #", g_tickCount, " — BTC bid=", bidPrice, " ask=", askPrice, 
               " spread=", NormalizeDouble(askPrice - bidPrice, 1));
   }
}

//+------------------------------------------------------------------+
//| Extract a double value from JSON by key                          |
//| Simple parser — finds "key": value pattern                       |
//+------------------------------------------------------------------+
double ExtractJsonDouble(const string &json, const string key)
{
   string searchKey = "\"" + key + "\"";
   int keyPos = StringFind(json, searchKey);
   if(keyPos < 0) return -1;
   
   // Find the colon after the key
   int colonPos = StringFind(json, ":", keyPos + StringLen(searchKey));
   if(colonPos < 0) return -1;
   
   // Find the start of the number (skip spaces and quotes)
   int numStart = colonPos + 1;
   int jsonLen = StringLen(json);
   
   while(numStart < jsonLen)
   {
      ushort ch = StringGetCharacter(json, numStart);
      if(ch == ' ' || ch == '"') 
         numStart++;
      else
         break;
   }
   
   // Find end of number
   int numEnd = numStart;
   while(numEnd < jsonLen)
   {
      ushort ch = StringGetCharacter(json, numEnd);
      if((ch >= '0' && ch <= '9') || ch == '.' || ch == '-')
         numEnd++;
      else
         break;
   }
   
   if(numEnd <= numStart) return -1;
   
   string numStr = StringSubstr(json, numStart, numEnd - numStart);
   return StringToDouble(numStr);
}

//+------------------------------------------------------------------+
//| OnTick — not used, we run on timer                               |
//+------------------------------------------------------------------+
void OnTick()
{
   // Price feeder runs on timer, not on ticks
}
//+------------------------------------------------------------------+
