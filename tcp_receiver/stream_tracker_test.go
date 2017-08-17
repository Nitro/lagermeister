package main

import (
	"os"
	"testing"

	. "github.com/SmartyStreets/goconvey/convey"
	"github.com/oxtoacart/bpool"
)

const (
	FixtureSize = 287
)

func Test_NewStreamTracker(t *testing.T) {
	Convey("NewStreamTracker()", t, func() {
		pool := bpool.NewBytePool(1, 128)
		stream := NewStreamTracker(pool)

		Convey("returns a tracker with an allocated buffer", func() {
			So(stream.buf, ShouldNotBeNil)
		})

		Convey("returns a tracker with the right buffer pool pointer", func() {
			So(stream.Pool, ShouldEqual, pool)
		})
	})
}

func Test_Read(t *testing.T) {
	Convey("Read()", t, func() {
		pool := bpool.NewBytePool(1, 512)
		stream := NewStreamTracker(pool)

		Convey("handles EOF", func() {
			input, _ := os.Open("fixtures/heka.pbuf")
			stream.Read(input)
			keepProcessing, finished := stream.Read(input)
			input.Close()

			So(keepProcessing, ShouldBeFalse)
			So(finished, ShouldBeTrue)
		})

		Convey("handles errors", func() {
			input, _ := os.Open("fixtures/heka.pbuf")
			// Won't return EOF, will just generate an error
			input.Close()
			keepProcessing, finished := stream.Read(input)

			So(keepProcessing, ShouldBeFalse)
			So(finished, ShouldBeTrue)
		})

		Convey("knows when to continue reading from the input", func() {
			stream.expectedLen = 511 // This should be bigger than the file size
			input, _ := os.Open("fixtures/heka.pbuf")
			keepProcessing, finished := stream.Read(input)
			input.Close()

			So(keepProcessing, ShouldBeTrue)
			So(finished, ShouldBeFalse)
		})
	})
}

func Test_HeadersAndMessages(t *testing.T) {
	Convey("Working with headers and messages in the incoming stream", t, func() {
		pool := bpool.NewBytePool(1, 16535)
		stream := NewStreamTracker(pool)
		input, err := os.Open("fixtures/heka.pbuf")
		So(err, ShouldBeNil)

		Reset(func() {
			input.Close()
		})

		Convey("FindHeader() finds the header when there is one", func() {
			stream.Read(input)
			So(stream.FindHeader(), ShouldBeTrue)
		})

		Convey("FindHeader() knows when it's missing", func() {
			So(stream.FindHeader(), ShouldBeFalse)
		})

		Convey("ParseHeader() complains when things aren't ready", func() {
			stream.Read(input)
			err = stream.ParseHeader()

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "called before")
		})

		Convey("ParseHeader() works when there is a message", func() {
			stream.Read(input)
			stream.FindHeader()
			err = stream.ParseHeader()

			So(err, ShouldBeNil)
			So(stream.header.GetMessageLength(), ShouldEqual, 281)
		})

		Convey("FindMessage() finds the message when there is one", func() {
			stream.Read(input)

			So(stream.FindMessage(), ShouldBeTrue)
		})

		Convey("FindMessage() knows when it's missing", func() {
			So(stream.FindMessage(), ShouldBeFalse)
		})

		Convey("ParseMessage() complains when things aren't ready", func() {
			stream.Read(input)
			ok, err := stream.ParseMessage()

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "called before")
			So(ok, ShouldBeFalse)
		})

		Convey("ParseMessage() parses a valid message", func() {
			stream.Read(input)
			stream.FindHeader()
			stream.ParseHeader()
			stream.FindMessage()
			ok, err := stream.ParseMessage()

			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)
			So(stream.msg.Payload, ShouldNotBeNil)
		})

		Convey("IsValid() correctly identifies reasonable-looking messages", func() {
			stream.Read(input)
			stream.FindHeader()
			stream.ParseHeader()
			stream.FindMessage()

			So(stream.IsValid(), ShouldBeTrue)
		})
	})
}

func Test_HandleOverread(t *testing.T) {
	Convey("HandleOverread()", t, func() {
		pool := bpool.NewBytePool(1, 16535)
		stream := NewStreamTracker(pool)

		Convey("identifies when it hasn't happened", func() {
			// There is only one record in this fixture, so can't overread
			input, _ := os.Open("fixtures/heka.pbuf")
			stream.Read(input)
			input.Close()

			So(stream.HandleOverread(), ShouldBeFalse)
		})

		Convey("identifies when it has happened", func() {
			input, _ := os.Open("fixtures/heka.pbuf")
			stream.Read(input)
			input.Seek(0, 0) // Reset to start so we can read it again
			stream.Read(input)
			input.Close()
			stream.FindHeader()
			stream.ParseHeader()

			So(stream.readLen, ShouldBeGreaterThan, FixtureSize) // More than one record was read
			So(stream.HandleOverread(), ShouldBeTrue)
		})

		Convey("leaves the buffer with only the new record in it", func() {
			input, _ := os.Open("fixtures/heka.pbuf")
			stream.Read(input)
			input.Seek(0, 0) // Reset to start so we can read it again
			stream.Read(input)
			input.Close()
			stream.FindHeader()
			stream.FindMessage()
			stream.ParseHeader()
			stream.ParseMessage()

			So(stream.readLen, ShouldEqual, 2*FixtureSize) // We read two records
			So(stream.HandleOverread(), ShouldBeTrue)
			So(stream.readLen, ShouldEqual, FixtureSize)   // Only one record remains
		})
	})
}
