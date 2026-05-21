//+------------------------------------------------------------------+
//|                                               JsonSerializer.mqh |
//|                        BTC Scalper Signal Generator               |
//|                        Lightweight JSON builder for MQL5          |
//+------------------------------------------------------------------+
#property copyright "BTC Scalper"
#property strict

#ifndef JSON_SERIALIZER_MQH
#define JSON_SERIALIZER_MQH

//+------------------------------------------------------------------+
//| CJsonBuilder — builds a flat JSON object via string concatenation |
//+------------------------------------------------------------------+
class CJsonBuilder
{
private:
   string            m_json;
   bool              m_firstField;

   //--- Escape special characters inside a string value
   string            EscapeString(const string value)
   {
      string result = value;
      // Backslash must be escaped first to avoid double-escaping
      StringReplace(result, "\\", "\\\\");
      StringReplace(result, "\"", "\\\"");
      StringReplace(result, "\n", "\\n");
      StringReplace(result, "\r", "\\r");
      StringReplace(result, "\t", "\\t");
      return result;
   }

   //--- Append the comma separator when needed
   void              PrependComma()
   {
      if(!m_firstField)
         m_json += ",";
      m_firstField = false;
   }

public:
                     CJsonBuilder() : m_json(""), m_firstField(true) {}

   //--- Start a new JSON object
   void              Begin()
   {
      m_json       = "{";
      m_firstField = true;
   }

   //--- Add a string field: "key":"value"
   void              AddString(const string key, const string value)
   {
      PrependComma();
      m_json += "\"" + key + "\":\"" + EscapeString(value) + "\"";
   }

   //--- Add a double field with controlled decimal digits
   void              AddDouble(const string key, const double value, const int digits)
   {
      PrependComma();
      m_json += "\"" + key + "\":" + DoubleToString(value, digits);
   }

   //--- Add a long (64-bit integer) field
   void              AddLong(const string key, const long value)
   {
      PrependComma();
      m_json += "\"" + key + "\":" + IntegerToString(value);
   }

   //--- Add an int field
   void              AddInt(const string key, const int value)
   {
      PrependComma();
      m_json += "\"" + key + "\":" + IntegerToString(value);
   }

   //--- Add a boolean field
   void              AddBool(const string key, const bool value)
   {
      PrependComma();
      m_json += "\"" + key + "\":" + (value ? "true" : "false");
   }

   //--- Close the object and return the full JSON string
   string            End()
   {
      m_json += "}";
      return m_json;
   }
};

#endif // JSON_SERIALIZER_MQH
//+------------------------------------------------------------------+
