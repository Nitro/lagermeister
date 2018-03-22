// Based on code extracted from Heka
// https://github.com/mozilla-services/heka/blob/278dd3d5961b9b6e47bb7a912b63ce3faaf8d8bd/plugins/elasticsearch/encoders.go

/***** BEGIN LICENSE BLOCK *****
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this file,
# You can obtain one at http://mozilla.org/MPL/2.0/.
#
# The Initial Developer of the Original Code is the Mozilla Foundation.
# Portions created by the Initial Developer are Copyright (C) 2013-2015
# the Initial Developer. All Rights Reserved.
#
# Contributor(s):
#   Tanguy Leroux (tlrx.dev@gmail.com)
#   Rob Miller (rmiller@mozilla.com)
#   Xavier Lange (xavier.lange@viasat.com)
#   John Staford (john@solinea.com)
#   Karl Matthias (karl.matthias@gonitro.com)
#
# ***** END LICENSE BLOCK *****/
package message

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mozilla-services/heka/build/heka/src/github.com/cactus/gostrftime"
)

const lowerhex = "0123456789abcdef"
const NEWLINE byte = 10

var defaultFieldMappings = ESFieldMappings{
	Timestamp:  "Timestamp",
	Uuid:       "Uuid",
	Type:       "Type",
	Logger:     "Logger",
	Severity:   "Severity",
	Payload:    "Payload",
	EnvVersion: "EnvVersion",
	Pid:        "Pid",
	Hostname:   "Hostname",
}

// Heka fields to ElasticSearch mapping
type ESFieldMappings struct {
	Timestamp  string
	Uuid       string
	Type       string
	Logger     string
	Severity   string
	Payload    string
	EnvVersion string
	Pid        string
	Hostname   string
}

// ESJsonEncode generates ElasticSearch/Logstash compatible JSON messages.
// Unlike the upstream Heka version, this does NOT handle binary blobs.
func ESJsonEncode(m *Message, buf bytes.Buffer, fields, dynamicFields []string, mappings *ESFieldMappings) (output []byte, err error) {
	// Use the default field mappings unless we're configured otherwise
	if mappings == nil {
		mappings = &defaultFieldMappings
	}

	buf.WriteString(`{`)
	bufLenBeforeFirstField := buf.Len()

	for _, f := range fields {
		first := buf.Len() == bufLenBeforeFirstField
		switch strings.ToLower(f) {
		case "uuid":
			writeStringField(first, &buf, mappings.Uuid, m.GetUuidString())
		case "timestamp":
			t := time.Unix(0, m.GetTimestamp()).UTC()
			writeStringField(first, &buf, mappings.Timestamp, gostrftime.Strftime("%Y-%m-%dT%H:%M:%S", t))
		case "type":
			writeStringField(first, &buf, mappings.Type, m.GetType())
		case "logger":
			writeStringField(first, &buf, mappings.Logger, m.GetLogger())
		case "severity":
			writeIntField(first, &buf, mappings.Severity, m.GetSeverity())
		case "payload":
			writeStringField(first, &buf, mappings.Payload, m.GetPayload())
		case "envversion":
			writeStringField(first, &buf, mappings.EnvVersion, m.GetEnvVersion())
		case "pid":
			writeIntField(first, &buf, mappings.Pid, m.GetPid())
		case "hostname":
			writeStringField(first, &buf, mappings.Hostname, m.GetHostname())
		case "dynamicfields":
			listsDynamicFields := len(dynamicFields) > 0

			for _, field := range m.Fields {
				dynamicFieldMatch := false
				if listsDynamicFields {
					for _, fieldName := range dynamicFields {
						if *field.Name == fieldName {
							dynamicFieldMatch = true
						}
					}
				} else {
					dynamicFieldMatch = true
				}

				if dynamicFieldMatch {
					raw := false
					writeField(first, &buf, field, raw, ".")
				}
			}
		default:
			err = fmt.Errorf("Unable to find field: %s", f)
			return
		}
	}

	buf.WriteString(`}`)
	buf.WriteByte(NEWLINE)
	return buf.Bytes(), err
}

func writeUTF16Escape(b *bytes.Buffer, c rune) {
	b.WriteString(`\u`)
	b.WriteByte(lowerhex[(c>>12)&0xF])
	b.WriteByte(lowerhex[(c>>8)&0xF])
	b.WriteByte(lowerhex[(c>>4)&0xF])
	b.WriteByte(lowerhex[c&0xF])
}

// Go json encoder will blow up on invalid utf8 so we use this custom json
// encoder. Also, go json encoder generates these funny \U escapes which I
// don't think are valid json.

// Also note that invalid utf-8 sequences get encoded as U+FFFD this is a
// feature. :)

func writeQuotedString(b *bytes.Buffer, str string) {
	b.WriteString(`"`)

	// string = quotation-mark *char quotation-mark

	// char = unescaped /
	//        escape (
	//            %x22 /          ; "    quotation mark  U+0022
	//            %x5C /          ; \    reverse solidus U+005C
	//            %x2F /          ; /    solidus         U+002F
	//            %x62 /          ; b    backspace       U+0008
	//            %x66 /          ; f    form feed       U+000C
	//            %x6E /          ; n    line feed       U+000A
	//            %x72 /          ; r    carriage return U+000D
	//            %x74 /          ; t    tab             U+0009
	//            %x75 4HEXDIG )  ; uXXXX                U+XXXX

	// escape = %x5C              ; \
	// quotation-mark = %x22      ; "

	// unescaped = %x20-21 / %x23-5B / %x5D-10FFFF

	for _, c := range str {
		if c == 0x20 || c == 0x21 || (c >= 0x23 && c <= 0x5B) || (c >= 0x5D) {
			b.WriteRune(c)
		} else {

			// All runes should be < 16 bits because of the (c >= 0x5D) guard
			// above. However, runes are int32 so it is possible to have
			// negative values that won't be correctly outputted. However,
			// afaik these values are not part of the unicode standard.
			writeUTF16Escape(b, c)
		}

	}
	b.WriteString(`"`)

}

func writeField(first bool, b *bytes.Buffer, f *Field, raw bool, replaceDotsWith string) {
	if !first {
		b.WriteString(`,`)
	}

	if replaceDotsWith != "." {
		writeQuotedString(b, strings.Replace(f.GetName(), ".", replaceDotsWith, -1))
	} else {
		writeQuotedString(b, f.GetName())
	}

	b.WriteString(`:`)

	switch f.GetValueType() {
	case Field_STRING:
		values := f.GetValueString()
		if len(values) > 1 {
			b.WriteString(`[`)
			for i, value := range values {
				if raw {
					b.WriteString(value)
				} else {
					writeQuotedString(b, value)
				}
				if i < len(values)-1 {
					b.WriteString(`,`)
				}
			}
			b.WriteString(`]`)
		} else {
			if raw {
				b.WriteString(values[0])
			} else {
				writeQuotedString(b, values[0])
			}
		}
	case Field_BYTES:
		values := f.GetValueBytes()
		if len(values) > 1 {
			b.WriteString(`[`)
			for i, value := range values {
				if raw {
					b.WriteString(string(value))
				} else {
					writeQuotedString(b, base64.StdEncoding.EncodeToString(value))
				}
				if i < len(values)-1 {
					b.WriteString(`,`)
				}
			}
			b.WriteString(`]`)
		} else {
			if raw {
				b.WriteString(string(values[0]))
			} else {
				writeQuotedString(b, string(values[0]))
			}
		}
	case Field_INTEGER:
		values := f.GetValueInteger()
		if len(values) > 1 {
			b.WriteString(`[`)
			for i, value := range values {
				b.WriteString(strconv.FormatInt(value, 10))
				if i < len(values)-1 {
					b.WriteString(`,`)
				}
			}
			b.WriteString(`]`)
		} else {
			b.WriteString(strconv.FormatInt(values[0], 10))
		}
	case Field_DOUBLE:
		values := f.GetValueDouble()
		if len(values) > 1 {
			b.WriteString(`[`)
			for i, value := range values {
				b.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
				if i < len(values)-1 {
					b.WriteString(`,`)
				}
			}
			b.WriteString(`]`)
		} else {
			b.WriteString(strconv.FormatFloat(values[0], 'g', -1, 64))
		}
	case Field_BOOL:
		values := f.GetValueBool()
		if len(values) > 1 {
			b.WriteString(`[`)
			for i, value := range values {
				b.WriteString(strconv.FormatBool(value))
				if i < len(values)-1 {
					b.WriteString(`,`)
				}
			}
			b.WriteString(`]`)
		} else {
			b.WriteString(strconv.FormatBool(values[0]))
		}
	}
}

func writeStringField(first bool, b *bytes.Buffer, name string, value string) {
	if !first {
		b.WriteString(`,`)
	}

	writeQuotedString(b, name)
	b.WriteString(`:`)
	writeQuotedString(b, value)
}

func writeIntField(first bool, b *bytes.Buffer, name string, value int32) {
	if !first {
		b.WriteString(`,`)
	}
	writeQuotedString(b, name)
	b.WriteString(`:`)
	b.WriteString(strconv.Itoa(int(value)))
}
