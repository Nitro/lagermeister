package main

import (
	"os"
	"testing"

	. "github.com/SmartyStreets/goconvey/convey"
	"github.com/oxtoacart/bpool"
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

func Test_FindingHeaderAndMessage(t *testing.T) {
	Convey("Finding the header and message in the incoming stream", t, func() {
		pool := bpool.NewBytePool(1, 16535)
		stream := NewStreamTracker(pool)

		Convey("FindHeader() finds the header when there is one", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			So(stream.FindHeader(), ShouldBeTrue)
		})

		Convey("ParseHeader() complains when things aren't ready", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			err = stream.ParseHeader()

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "called before")
		})

		Convey("ParseHeader() works when there is a message", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			stream.FindHeader()
			err = stream.ParseHeader()
			So(err, ShouldBeNil)

			So(stream.header.GetMessageLength(), ShouldEqual, 281)
		})

		Convey("FindMessage() finds the message when there is one", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			So(stream.FindMessage(), ShouldBeTrue)
		})

		Convey("ParseMessage() complains when things aren't ready", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			ok, err := stream.ParseMessage()

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "called before")
			So(ok, ShouldBeFalse)
		})

		Convey("ParseMessage() parses a valid message", func() {
			input, err := os.Open("fixtures/heka.pbuf")
			So(err, ShouldBeNil)

			stream.Read(input)
			input.Close()

			stream.FindHeader()
			stream.ParseHeader()
			stream.FindMessage()
			ok, err := stream.ParseMessage()

			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)
			So(stream.msg.Payload, ShouldNotBeNil)
		})
	})
}
