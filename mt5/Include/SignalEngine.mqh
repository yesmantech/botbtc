//+------------------------------------------------------------------+
//|                                                 SignalEngine.mqh |
//|                        BTC Scalper Signal Generator               |
//|                        Signal evaluation and ID generation        |
//+------------------------------------------------------------------+
#property copyright "BTC Scalper"
#property strict

#ifndef SIGNAL_ENGINE_MQH
#define SIGNAL_ENGINE_MQH

#include "FeatureEngine.mqh"

//+------------------------------------------------------------------+
//| Signal type enumeration                                           |
//+------------------------------------------------------------------+
enum ENUM_SIGNAL_TYPE
{
   SIGNAL_NONE  = 0,
   SIGNAL_LONG  = 1,
   SIGNAL_SHORT = 2
};

//+------------------------------------------------------------------+
//| Signal evaluation parameters                                      |
//+------------------------------------------------------------------+
struct SignalParams
{
   double            minVelocity250;
   double            minVelocity500;
   double            maxSpread;
   double            minATR;
   double            maxATR;
};

//+------------------------------------------------------------------+
//| CSignalEngine — evaluates tick features against thresholds       |
//+------------------------------------------------------------------+
class CSignalEngine
{
private:
   SignalParams      m_params;
   int               m_signalSeq;           // sequential counter for signal_id
   long              m_lastSignalTimeMs;     // last signal timestamp (ms) for cooldown
   int               m_cooldownMs;           // cooldown period in milliseconds

public:
                     CSignalEngine();
   void              Init(SignalParams &params);
   void              SetCooldownMs(int cooldown_ms) { m_cooldownMs = cooldown_ms; }
   ENUM_SIGNAL_TYPE  Evaluate(CFeatureEngine &features, double bid, double ask, double spread);
   string            GenerateSignalID(int magic);

   //--- Cooldown support ---
   bool              IsInCooldown(long currentTimeMs);
   void              RecordSignal(long currentTimeMs);
};

//+------------------------------------------------------------------+
//| Constructor                                                       |
//+------------------------------------------------------------------+
CSignalEngine::CSignalEngine()
   : m_signalSeq(0),
     m_lastSignalTimeMs(0),
     m_cooldownMs(1000)
{
   ZeroMemory(m_params);
}

//+------------------------------------------------------------------+
//| Init — set evaluation thresholds                                 |
//+------------------------------------------------------------------+
void CSignalEngine::Init(SignalParams &params)
{
   m_params    = params;
   m_signalSeq = 0;
}

//+------------------------------------------------------------------+
//| IsInCooldown — check if enough time has passed since last signal  |
//+------------------------------------------------------------------+
bool CSignalEngine::IsInCooldown(long currentTimeMs)
{
   if(m_lastSignalTimeMs == 0)
      return false;   // no signal ever sent
   return (currentTimeMs - m_lastSignalTimeMs) < (long)m_cooldownMs;
}

//+------------------------------------------------------------------+
//| RecordSignal — mark the time of the latest signal                |
//+------------------------------------------------------------------+
void CSignalEngine::RecordSignal(long currentTimeMs)
{
   m_lastSignalTimeMs = currentTimeMs;
}

//+------------------------------------------------------------------+
//| Evaluate — check feature values against signal conditions        |
//|                                                                    |
//| LONG conditions:                                                   |
//|   velocity250 > minVelocity250                                    |
//|   velocity500 > minVelocity500                                    |
//|   emaFast > emaSlow                                                |
//|   ATR in [minATR, maxATR]                                         |
//|   spread < maxSpread                                               |
//|                                                                    |
//| SHORT conditions:                                                  |
//|   velocity250 < -minVelocity250                                   |
//|   velocity500 < -minVelocity500                                   |
//|   emaFast < emaSlow                                                |
//|   ATR in [minATR, maxATR]                                         |
//|   spread < maxSpread                                               |
//+------------------------------------------------------------------+
ENUM_SIGNAL_TYPE CSignalEngine::Evaluate(CFeatureEngine &features,
                                          double bid, double ask, double spread)
{
   // --- Feature engine must be ready ---
   if(!features.IsReady())
   {
      return SIGNAL_NONE;
   }

   // --- Common filters: ATR range and spread cap ---
   double atr = features.GetMicroATR1s();
   if(atr < m_params.minATR || atr > m_params.maxATR)
      return SIGNAL_NONE;

   if(spread > m_params.maxSpread)
      return SIGNAL_NONE;

   double vel250 = features.GetVelocity250();
   double vel500 = features.GetVelocity500();
   double emaF   = features.GetEmaFast();
   double emaS   = features.GetEmaSlow();

   // --- Minimum absolute velocity check ---
   if(MathAbs(vel250) < m_params.minVelocity250)
      return SIGNAL_NONE;
   if(MathAbs(vel500) < m_params.minVelocity500)
      return SIGNAL_NONE;

   // --- LONG ---
   if(vel250 > m_params.minVelocity250 &&
      vel500 > m_params.minVelocity500 &&
      emaF   > emaS)
   {
      return SIGNAL_LONG;
   }

   // --- SHORT ---
   if(vel250 < -m_params.minVelocity250 &&
      vel500 < -m_params.minVelocity500 &&
      emaF   < emaS)
   {
      return SIGNAL_SHORT;
   }

   return SIGNAL_NONE;
}

//+------------------------------------------------------------------+
//| GenerateSignalID — "SIG-{magic}-{YYYYMMDD}-{seq:06d}"           |
//+------------------------------------------------------------------+
string CSignalEngine::GenerateSignalID(int magic)
{
   m_signalSeq++;

   MqlDateTime dt;
   TimeCurrent(dt);

   string date_part = StringFormat("%04d%02d%02d", dt.year, dt.mon, dt.day);
   string seq_part  = StringFormat("%06d", m_signalSeq);

   return "SIG-" + IntegerToString(magic) + "-" + date_part + "-" + seq_part;
}

#endif // SIGNAL_ENGINE_MQH
//+------------------------------------------------------------------+
