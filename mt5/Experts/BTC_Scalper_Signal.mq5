//+------------------------------------------------------------------+
//|                                          BTC_Scalper_Signal.mq5  |
//|                        BTC Scalper Signal Generator               |
//|                        Generates signals → TCP bridge. No trades. |
//+------------------------------------------------------------------+
#property copyright   "BTC Scalper"
#property link        ""
#property version     "2.00"
#property description "BTC Scalper — tick-level signal generator v2."
#property description "Sends JSON signals to Python bridge via TCP socket."
#property description "Does NOT execute any trades on MT5."
#property description "Press K to toggle kill switch (stop/resume signals)."
#property strict

//+------------------------------------------------------------------+
//| Includes                                                          |
//+------------------------------------------------------------------+
#include <BridgeClient.mqh>
#include <JsonSerializer.mqh>
#include <FeatureEngine.mqh>
#include <SignalEngine.mqh>

//+------------------------------------------------------------------+
//| Input parameters                                                  |
//+------------------------------------------------------------------+
input string BridgeHost          = "127.0.0.1";    // Bridge TCP host
input int    BridgePort          = 9090;            // Bridge TCP port
input int    SocketTimeout       = 3000;            // Socket connect timeout (ms)
input int    ReconnectIntervalSec= 5;               // Reconnect throttle (sec)
input double MinVelocity250      = 5.0;             // Min velocity 250ms threshold
input double MinVelocity500      = 3.0;             // Min velocity 500ms threshold
input double MaxSpread           = 50.0;            // Max allowed spread (points)
input double MinATR              = 2.0;             // Min micro-ATR 1s
input double MaxATR              = 200.0;           // Max micro-ATR 1s
input int    MagicNumber         = 20260521;         // Magic number for signal IDs
input int    RingBufferSize      = 2000;            // Tick ring buffer capacity
input double EmaFastPeriod       = 10.0;            // EMA fast period (ticks)
input double EmaSlowPeriod       = 30.0;            // EMA slow period (ticks)
input int    CooldownMs          = 1000;            // Cooldown between signals (ms)
input int    MinTickCount        = 100;             // Min ticks before first signal

//+------------------------------------------------------------------+
//| Globals                                                           |
//+------------------------------------------------------------------+
CBridgeClient  g_bridge;
CFeatureEngine g_features;
CSignalEngine  g_signals;
long           g_ticksSeen      = 0;
long           g_signalsSent    = 0;
bool           g_initialized    = false;
bool           g_killSwitch     = false;    // manual kill switch (press K)

//+------------------------------------------------------------------+
//| Expert initialization                                             |
//+------------------------------------------------------------------+
int OnInit()
{
   // --- Validate symbol ---
   string sym = Symbol();
   PrintFormat("[EA] Starting on %s  |  MagicNumber=%d  |  CooldownMs=%d  |  MinTickCount=%d",
               sym, MagicNumber, CooldownMs, MinTickCount);

   // --- Initialize feature engine ---
   g_features.Init(RingBufferSize, EmaFastPeriod, EmaSlowPeriod);
   g_features.SetMinTickCount(MinTickCount);

   // --- Initialize signal engine ---
   SignalParams sp;
   sp.minVelocity250 = MinVelocity250;
   sp.minVelocity500 = MinVelocity500;
   sp.maxSpread      = MaxSpread;
   sp.minATR         = MinATR;
   sp.maxATR         = MaxATR;
   g_signals.Init(sp);
   g_signals.SetCooldownMs(CooldownMs);

   // --- Configure bridge reconnect interval ---
   g_bridge.SetReconnectInterval(ReconnectIntervalSec);

   // --- Connect to bridge ---
   if(!g_bridge.Connect(BridgeHost, BridgePort, SocketTimeout))
   {
      PrintFormat("[EA] Initial bridge connection failed – will retry via OnTimer");
   }
   else
   {
      PrintFormat("[EA] Bridge connected on init");
   }

   // --- Start 1-second health-check timer ---
   EventSetTimer(1);

   // --- Enable chart events for kill switch ---
   ChartSetInteger(0, CHART_EVENT_KEYBOARD, true);

   g_initialized = true;
   g_killSwitch  = false;

   // --- Initial chart comment ---
   UpdateChartComment();

   return INIT_SUCCEEDED;
}

//+------------------------------------------------------------------+
//| Expert deinitialization                                           |
//+------------------------------------------------------------------+
void OnDeinit(const int reason)
{
   EventKillTimer();
   g_bridge.Disconnect();
   PrintFormat("[EA] Deinit  reason=%d  |  ticks=%lld  signals=%lld  bridge_sent=%d  bridge_recv=%d",
               reason, g_ticksSeen, g_signalsSent,
               g_bridge.GetMessagesSent(), g_bridge.GetMessagesReceived());
}

//+------------------------------------------------------------------+
//| Chart comment helper — displays live status on chart              |
//+------------------------------------------------------------------+
void UpdateChartComment()
{
   string killStatus = g_killSwitch ? "STOPPED (press K to resume)" : "ACTIVE";
   string connStatus = g_bridge.IsConnected() ? "Connected" : "Disconnected";
   string readyStatus = g_features.IsReady() ? "Ready" : StringFormat("Warming up (%d/%d)", g_features.GetTickCount(), MinTickCount);

   Comment(StringFormat(
      "═══ BTC Scalper Signal v2.0 ═══\n"
      "Symbol:        %s\n"
      "Signals:       %s\n"
      "Engine:        %s\n"
      "Bridge:        %s\n"
      "Ticks:         %lld\n"
      "Signals sent:  %lld\n"
      "Bridge msgs:   sent=%d  recv=%d\n"
      "Uptime:        %lld s",
      Symbol(),
      killStatus,
      readyStatus,
      connStatus,
      g_ticksSeen,
      g_signalsSent,
      g_bridge.GetMessagesSent(),
      g_bridge.GetMessagesReceived(),
      g_bridge.GetUptimeMs() / 1000
   ));
}

//+------------------------------------------------------------------+
//| Expert tick handler — main hot path                              |
//+------------------------------------------------------------------+
void OnTick()
{
   if(!g_initialized)
      return;

   // --- Read current tick ---
   MqlTick tick;
   if(!SymbolInfoTick(Symbol(), tick))
   {
      Print("[EA] SymbolInfoTick failed – skipping tick");
      return;
   }

   g_ticksSeen++;

   // --- Millisecond timestamp ---
   long tick_time_ms = (long)tick.time_msc;  // MqlTick.time_msc is already in ms

   double bid    = tick.bid;
   double ask    = tick.ask;
   double spread = ask - bid;

   // --- Update feature engine ---
   g_features.Update(bid, ask, tick_time_ms);

   // --- Update chart comment every 100 ticks ---
   if(g_ticksSeen % 100 == 0)
      UpdateChartComment();

   // --- Kill switch active — skip signal generation ---
   if(g_killSwitch)
      return;

   // --- Feature engine must be ready (enough ticks buffered) ---
   if(!g_features.IsReady())
      return;

   // --- Cooldown check — prevent signal flood ---
   if(g_signals.IsInCooldown(tick_time_ms))
      return;

   // --- Evaluate signal ---
   ENUM_SIGNAL_TYPE sig = g_signals.Evaluate(g_features, bid, ask, spread);
   if(sig == SIGNAL_NONE)
      return;

   // --- Build JSON payload ---
   if(!g_bridge.IsConnected())
   {
      Print("[EA] Signal generated but bridge not connected – discarding");
      return;  // don't build payload if we can't send it
   }

   string signal_id   = g_signals.GenerateSignalID(MagicNumber);
   string signal_type = (sig == SIGNAL_LONG) ? "LONG" : "SHORT";

   // --- Read symbol info ---
   double contract_size = SymbolInfoDouble(_Symbol, SYMBOL_TRADE_CONTRACT_SIZE);
   double volume_min    = SymbolInfoDouble(_Symbol, SYMBOL_VOLUME_MIN);
   double volume_max    = SymbolInfoDouble(_Symbol, SYMBOL_VOLUME_MAX);
   double volume_step   = SymbolInfoDouble(_Symbol, SYMBOL_VOLUME_STEP);
   double point_val     = SymbolInfoDouble(_Symbol, SYMBOL_POINT);
   int    digits_val    = (int)SymbolInfoInteger(_Symbol, SYMBOL_DIGITS);

   CJsonBuilder json;
   json.Begin();
   json.AddString("signal_id",      signal_id);
   json.AddString("symbol",         Symbol());
   json.AddString("signal",         signal_type);
   json.AddDouble("bid",            bid,    2);
   json.AddDouble("ask",            ask,    2);
   json.AddDouble("spread",         spread, 2);
   json.AddDouble("velocity250",    g_features.GetVelocity250(),  4);
   json.AddDouble("velocity500",    g_features.GetVelocity500(),  4);
   json.AddDouble("velocity1000",   g_features.GetVelocity1000(), 4);
   json.AddDouble("acceleration",   g_features.GetAcceleration(), 4);
   json.AddDouble("ema_fast",       g_features.GetEmaFast(),      4);
   json.AddDouble("ema_slow",       g_features.GetEmaSlow(),      4);
   json.AddDouble("micro_atr_1s",   g_features.GetMicroATR1s(),   4);
   json.AddDouble("micro_atr_2s",   g_features.GetMicroATR2s(),   4);
   json.AddDouble("contract_size",  contract_size, 8);
   json.AddDouble("volume_min",     volume_min,    8);
   json.AddDouble("volume_max",     volume_max,    8);
   json.AddDouble("volume_step",    volume_step,   8);
   json.AddDouble("point",          point_val,     8);
   json.AddInt   ("digits",         digits_val);
   json.AddLong  ("timestamp_ms",   tick_time_ms);
   json.AddInt   ("magic",          MagicNumber);
   string payload = json.End();

   // --- Send signal ---
   if(g_bridge.SendSignal(payload))
   {
      g_signalsSent++;
      g_signals.RecordSignal(tick_time_ms);
      PrintFormat("[EA] SIGNAL #%lld %s  id=%s  bid=%.2f  ask=%.2f  v250=%.4f  v500=%.4f  atr=%.4f",
                  g_signalsSent, signal_type, signal_id, bid, ask,
                  g_features.GetVelocity250(),
                  g_features.GetVelocity500(),
                  g_features.GetMicroATR1s());
      UpdateChartComment();
   }
   else
   {
      PrintFormat("[EA] Failed to send signal %s – bridge error", signal_id);
   }
}

//+------------------------------------------------------------------+
//| Timer handler — health check & reconnect                         |
//+------------------------------------------------------------------+
void OnTimer()
{
   if(!g_bridge.IsConnected())
   {
      g_bridge.TryReconnect();
   }
   else
   {
      // Lightweight heartbeat: send a ping JSON
      CJsonBuilder json;
      json.Begin();
      json.AddString("type", "heartbeat");
      json.AddLong  ("timestamp_ms", (long)GetTickCount64());
      json.AddLong  ("ticks_seen",   g_ticksSeen);
      json.AddLong  ("signals_sent", g_signalsSent);
      json.AddBool  ("kill_switch",  g_killSwitch);
      json.AddInt   ("bridge_sent",  g_bridge.GetMessagesSent());
      json.AddInt   ("bridge_recv",  g_bridge.GetMessagesReceived());
      json.AddLong  ("uptime_ms",    g_bridge.GetUptimeMs());
      string hb = json.End();

      if(!g_bridge.Send(hb))
      {
         PrintFormat("[EA] Heartbeat send failed – marking disconnected");
         // Connection will be restored on next TryReconnect cycle
      }
   }

   // Refresh chart comment every second
   UpdateChartComment();
}

//+------------------------------------------------------------------+
//| Chart event handler — kill switch (K key)                        |
//+------------------------------------------------------------------+
void OnChartEvent(const int id,
                  const long &lparam,
                  const double &dparam,
                  const string &sparam)
{
   if(id == CHARTEVENT_KEYDOWN)
   {
      // 'K' key = ASCII 75
      if(lparam == 75)
      {
         g_killSwitch = !g_killSwitch;
         if(g_killSwitch)
         {
            PrintFormat("[EA] ⛔ KILL SWITCH ACTIVATED — signals paused");
         }
         else
         {
            PrintFormat("[EA] ✅ KILL SWITCH DEACTIVATED — signals resumed");
         }
         UpdateChartComment();
      }
   }
}
//+------------------------------------------------------------------+
