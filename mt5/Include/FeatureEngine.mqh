//+------------------------------------------------------------------+
//|                                                FeatureEngine.mqh |
//|                        BTC Scalper Signal Generator               |
//|                        Tick-based feature calculator with ring    |
//|                        buffer, velocity, EMA, micro-ATR           |
//+------------------------------------------------------------------+
#property copyright "BTC Scalper"
#property strict

#ifndef FEATURE_ENGINE_MQH
#define FEATURE_ENGINE_MQH

//+------------------------------------------------------------------+
//| CFeatureEngine — ring-buffer-backed tick feature calculator      |
//+------------------------------------------------------------------+
class CFeatureEngine
{
private:
   // --- Ring buffer ---
   double            m_midPrices[];   // circular buffer of mid prices
   long              m_tickTimes[];   // ms timestamps for each entry
   int               m_bufferSize;    // capacity
   int               m_head;          // write position (next slot to fill)
   int               m_count;         // number of valid entries (≤ bufferSize)

   // --- Current computed features ---
   double            m_velocity250;
   double            m_velocity500;
   double            m_velocity1000;
   double            m_acceleration;
   double            m_emaFast;
   double            m_emaSlow;
   double            m_microATR1s;
   double            m_microATR2s;

   // --- EMA state ---
   double            m_emaFastAlpha;
   double            m_emaSlowAlpha;
   bool              m_emaInitialized;

   // --- Min tick count for readiness ---
   int               m_minTickCount;

   //--- Private helpers
   double            GetPriceAtAge(long age_ms);
   double            CalcMicroATR(long window_ms);

   //--- Ring-buffer index helpers
   int               OldestIndex()    { return (m_count < m_bufferSize) ? 0 : m_head; }
   int               NewestIndex()    { return (m_head - 1 + m_bufferSize) % m_bufferSize; }
   int               PrevIndex(int i) { return (i - 1 + m_bufferSize) % m_bufferSize; }

public:
                     CFeatureEngine();
                    ~CFeatureEngine();
   void              Init(int buffer_size, double ema_fast_period, double ema_slow_period);
   void              SetMinTickCount(int min_ticks) { m_minTickCount = min_ticks; }
   void              Update(double bid, double ask, long tick_time_ms);

   // --- Readiness check ---
   bool              IsReady()         { return (m_count >= m_minTickCount); }

   // --- Getters ---
   double            GetVelocity250()  { return m_velocity250;  }
   double            GetVelocity500()  { return m_velocity500;  }
   double            GetVelocity1000() { return m_velocity1000; }
   double            GetAcceleration() { return m_acceleration; }
   double            GetEmaFast()      { return m_emaFast;      }
   double            GetEmaSlow()      { return m_emaSlow;      }
   double            GetMicroATR1s()   { return m_microATR1s;   }
   double            GetMicroATR2s()   { return m_microATR2s;   }
   int               GetTickCount()    { return m_count;         }

   // --- Mid price and timestamp ---
   double            GetMidPrice();
   long              GetLatestTickTimeMs();
};

//+------------------------------------------------------------------+
//| Constructor                                                       |
//+------------------------------------------------------------------+
CFeatureEngine::CFeatureEngine()
   : m_bufferSize(0),
     m_head(0),
     m_count(0),
     m_velocity250(0.0),
     m_velocity500(0.0),
     m_velocity1000(0.0),
     m_acceleration(0.0),
     m_emaFast(0.0),
     m_emaSlow(0.0),
     m_microATR1s(0.0),
     m_microATR2s(0.0),
     m_emaFastAlpha(0.0),
     m_emaSlowAlpha(0.0),
     m_emaInitialized(false),
     m_minTickCount(100)
{
}

//+------------------------------------------------------------------+
//| Destructor                                                        |
//+------------------------------------------------------------------+
CFeatureEngine::~CFeatureEngine()
{
}

//+------------------------------------------------------------------+
//| Init — allocate ring buffer and compute EMA alphas               |
//+------------------------------------------------------------------+
void CFeatureEngine::Init(int buffer_size, double ema_fast_period, double ema_slow_period)
{
   m_bufferSize = buffer_size;
   ArrayResize(m_midPrices, m_bufferSize);
   ArrayResize(m_tickTimes, m_bufferSize);
   ArrayInitialize(m_midPrices, 0.0);
   ArrayInitialize(m_tickTimes, 0);

   m_head  = 0;
   m_count = 0;

   // EMA smoothing factor: alpha = 2 / (N + 1)
   m_emaFastAlpha  = 2.0 / (ema_fast_period + 1.0);
   m_emaSlowAlpha  = 2.0 / (ema_slow_period + 1.0);
   m_emaInitialized = false;

   m_velocity250  = 0.0;
   m_velocity500  = 0.0;
   m_velocity1000 = 0.0;
   m_acceleration = 0.0;
   m_emaFast      = 0.0;
   m_emaSlow      = 0.0;
   m_microATR1s   = 0.0;
   m_microATR2s   = 0.0;
}

//+------------------------------------------------------------------+
//| GetMidPrice — return the current (most recent) mid price         |
//+------------------------------------------------------------------+
double CFeatureEngine::GetMidPrice()
{
   if(m_count == 0)
      return 0.0;
   return m_midPrices[NewestIndex()];
}

//+------------------------------------------------------------------+
//| GetLatestTickTimeMs — return the most recent tick timestamp (ms)  |
//+------------------------------------------------------------------+
long CFeatureEngine::GetLatestTickTimeMs()
{
   if(m_count == 0)
      return 0;
   return m_tickTimes[NewestIndex()];
}

//+------------------------------------------------------------------+
//| Update — push new tick, recompute all features                   |
//+------------------------------------------------------------------+
void CFeatureEngine::Update(double bid, double ask, long tick_time_ms)
{
   double mid = (bid + ask) / 2.0;

   // --- Write into ring buffer at head position ---
   m_midPrices[m_head] = mid;
   m_tickTimes[m_head] = tick_time_ms;
   m_head = (m_head + 1) % m_bufferSize;
   if(m_count < m_bufferSize)
      m_count++;

   // --- Need at least 2 ticks for any meaningful feature ---
   if(m_count < 2)
   {
      // Seed EMAs with first mid price
      if(!m_emaInitialized)
      {
         m_emaFast  = mid;
         m_emaSlow  = mid;
         m_emaInitialized = true;
      }
      return;
   }

   // --- If not yet ready (below minTickCount), still accumulate but zero features ---
   if(m_count < m_minTickCount)
   {
      // Update EMAs to keep them warm, but zero the published features
      if(!m_emaInitialized)
      {
         m_emaFast  = mid;
         m_emaSlow  = mid;
         m_emaInitialized = true;
      }
      else
      {
         m_emaFast = m_emaFastAlpha * mid + (1.0 - m_emaFastAlpha) * m_emaFast;
         m_emaSlow = m_emaSlowAlpha * mid + (1.0 - m_emaSlowAlpha) * m_emaSlow;
      }
      m_velocity250  = 0.0;
      m_velocity500  = 0.0;
      m_velocity1000 = 0.0;
      m_acceleration = 0.0;
      m_microATR1s   = 0.0;
      m_microATR2s   = 0.0;
      return;
   }

   // --- Velocity: price change over time window ---
   double mid_250  = GetPriceAtAge(250);
   double mid_500  = GetPriceAtAge(500);
   double mid_1000 = GetPriceAtAge(1000);

   m_velocity250  = (mid_250  != 0.0) ? (mid - mid_250)  : 0.0;
   m_velocity500  = (mid_500  != 0.0) ? (mid - mid_500)  : 0.0;
   m_velocity1000 = (mid_1000 != 0.0) ? (mid - mid_1000) : 0.0;

   // --- Acceleration: difference between fast and slow velocity ---
   m_acceleration = m_velocity250 - m_velocity1000;

   // --- EMA update ---
   if(!m_emaInitialized)
   {
      m_emaFast  = mid;
      m_emaSlow  = mid;
      m_emaInitialized = true;
   }
   else
   {
      m_emaFast = m_emaFastAlpha * mid + (1.0 - m_emaFastAlpha) * m_emaFast;
      m_emaSlow = m_emaSlowAlpha * mid + (1.0 - m_emaSlowAlpha) * m_emaSlow;
   }

   // --- Micro ATR ---
   m_microATR1s = CalcMicroATR(1000);   // 1-second window
   m_microATR2s = CalcMicroATR(2000);   // 2-second window
}

//+------------------------------------------------------------------+
//| GetPriceAtAge — find the mid price closest to (now - age_ms)     |
//|                 by scanning backward through the ring buffer      |
//+------------------------------------------------------------------+
double CFeatureEngine::GetPriceAtAge(long age_ms)
{
   if(m_count < 2)
      return 0.0;

   int newest = NewestIndex();
   long now_ms = m_tickTimes[newest];
   long target_ms = now_ms - age_ms;

   // If the oldest tick is newer than our target, we don't have data that old
   int oldest = OldestIndex();
   if(m_tickTimes[oldest] > target_ms)
   {
      // Return oldest available as best approximation, only if the buffer
      // covers at least half the requested window
      long actual_span = now_ms - m_tickTimes[oldest];
      if(actual_span >= age_ms / 2)
         return m_midPrices[oldest];
      return 0.0;
   }

   // Scan backward from newest to find the entry closest to target_ms
   double best_price = 0.0;
   long   best_diff  = LONG_MAX;

   int idx = newest;
   for(int i = 0; i < m_count; i++)
   {
      long diff = MathAbs(m_tickTimes[idx] - target_ms);
      if(diff < best_diff)
      {
         best_diff  = diff;
         best_price = m_midPrices[idx];
      }
      // Once we pass beyond the target going backward, no need to continue
      if(m_tickTimes[idx] < target_ms)
         break;

      idx = PrevIndex(idx);
   }

   return best_price;
}

//+------------------------------------------------------------------+
//| CalcMicroATR — average |Δprice| for ticks within window_ms       |
//+------------------------------------------------------------------+
double CFeatureEngine::CalcMicroATR(long window_ms)
{
   if(m_count < 3)
      return 0.0;

   int newest = NewestIndex();
   long now_ms = m_tickTimes[newest];
   long cutoff = now_ms - window_ms;

   double sum_abs_change = 0.0;
   int    change_count   = 0;

   int idx      = newest;
   int prev_idx = PrevIndex(idx);

   for(int i = 1; i < m_count; i++) // start at 1 because we need a pair
   {
      // Stop when the previous tick is outside the window
      if(m_tickTimes[prev_idx] < cutoff)
         break;

      double delta = MathAbs(m_midPrices[idx] - m_midPrices[prev_idx]);
      sum_abs_change += delta;
      change_count++;

      idx      = prev_idx;
      prev_idx = PrevIndex(idx);
   }

   if(change_count == 0)
      return 0.0;

   return sum_abs_change / (double)change_count;
}

#endif // FEATURE_ENGINE_MQH
//+------------------------------------------------------------------+
