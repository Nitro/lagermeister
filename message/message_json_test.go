package message

import (
	"bytes"
	"testing"
	"time"

	"github.com/pborman/uuid"
	. "github.com/smartystreets/goconvey/convey"
)

func Test_ESJsonEncode(t *testing.T) {
	Convey("ESJsonEncode", t, func() {
		m := getTestMessageWithFunnyFields()
		buf := bytes.Buffer{}
		fields := []string{
			"uuid", "timestamp", "type", "logger", "severity", "payload", "pid", "hostname",
		}
		dynamicFields := []string{}

		Convey("generates the right output for a whole message", func() {
			output, err := ESJsonEncode(m, buf, fields, dynamicFields, nil)

			So(err, ShouldBeNil)
			So(string(output), ShouldEqual,
				`{"Uuid":"87cf1ac2-e810-4ddf-a02d-a5ce44d13a85","Timestamp":"2013-07-16T15:49:05","Type":"TEST","Logger":"GoSpec","Severity":6,"Payload":"Test Payload","Pid":14098,"Hostname":"hostname"}`+"\n",
			)
		})

		Convey("handles some funny fields properly", func() {
			fields = append(fields, "dynamicFields")
			dynamicFields := []string{
				"integerArray", "doubleArray", "test.dotted.field.name.string",
			}
			output, err := ESJsonEncode(m, buf, fields, dynamicFields, nil)

			So(err, ShouldBeNil)
			So(string(output), ShouldEqual,
				`{"Uuid":"87cf1ac2-e810-4ddf-a02d-a5ce44d13a85","Timestamp":"2013-07-16T15:49:05","Type":"TEST","Logger":"GoSpec","Severity":6,"Payload":"Test Payload","Pid":14098,"Hostname":"hostname","integerArray":[22,80,3000],"doubleArray":[42,19101.3],"test.dotted.field.name.string":"{\u0022asdf\u0022:123}"}`+"\n",
			)
		})
	})
}

// From Heka test suite. Generates a message with lots of possible issues
func getTestMessageWithFunnyFields() *Message {
	field, _ := NewField(`"foo`, "bar\n", "")
	field1, _ := NewField(`"number`, 64, "")
	field2, _ := NewField("\xa3", "\xa3", "")
	field3, _ := NewField("idField", "1234", "")
	field4 := NewFieldInit("test_raw_field_bytes_array", Field_BYTES, "")
	field4.AddValue([]byte("{\"asdf\":123}"))
	field4.AddValue([]byte("{\"jkl;\":123}"))
	field5 := NewFieldInit("byteArray", Field_BYTES, "")
	field5.AddValue([]byte("asdf"))
	field5.AddValue([]byte("jkl;"))
	field6 := NewFieldInit("integerArray", Field_INTEGER, "")
	field6.AddValue(22)
	field6.AddValue(80)
	field6.AddValue(3000)
	field7 := NewFieldInit("doubleArray", Field_DOUBLE, "")
	field7.AddValue(42.0)
	field7.AddValue(19101.3)
	field8 := NewFieldInit("boolArray", Field_BOOL, "")
	field8.AddValue(true)
	field8.AddValue(false)
	field8.AddValue(false)
	field8.AddValue(false)
	field8.AddValue(true)
	field9 := NewFieldInit("test_raw_field_string", Field_STRING, "")
	field9.AddValue("{\"asdf\":123}")
	field10 := NewFieldInit("test_raw_field_bytes", Field_BYTES, "")
	field10.AddValue([]byte("{\"asdf\":123}"))
	field11 := NewFieldInit("test_raw_field_string_array", Field_STRING, "")
	field11.AddValue("{\"asdf\":123}")
	field11.AddValue("{\"jkl;\":123}")
	field12 := NewFieldInit("stringArray", Field_STRING, "")
	field12.AddValue("asdf")
	field12.AddValue("jkl;")
	field12.AddValue("push")
	field12.AddValue("pull")
	field13 := NewFieldInit("test.dotted.field.name.string", Field_STRING, "")
	field13.AddValue("{\"asdf\":123}")

	msg := &Message{}
	msg.SetType("TEST")
	loc, _ := time.LoadLocation("UTC")
	t, _ := time.ParseInLocation("2006-01-02T15:04:05", "2013-07-16T15:49:05",
		loc)
	msg.SetTimestamp(t.UnixNano())
	msg.SetUuid(uuid.Parse("87cf1ac2-e810-4ddf-a02d-a5ce44d13a85"))
	msg.SetLogger("GoSpec")
	msg.SetSeverity(int32(6))
	msg.SetPayload("Test Payload")
	msg.SetEnvVersion("0.8")
	msg.SetPid(14098)
	msg.SetHostname("hostname")
	msg.AddField(field)
	msg.AddField(field1)
	msg.AddField(field2)
	msg.AddField(field3)
	msg.AddField(field4)
	msg.AddField(field5)
	msg.AddField(field6)
	msg.AddField(field7)
	msg.AddField(field8)
	msg.AddField(field9)
	msg.AddField(field10)
	msg.AddField(field11)
	msg.AddField(field12)
	msg.AddField(field13)

	return msg
}
