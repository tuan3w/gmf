package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/tuan3w/gmf"
)

func fatal(err error) {
	log.Fatal(err)
}

func main() {
	outputfilename := "sample-encoding.mpg"
	dstWidth, dstHeight := 640, 480

	codec, err := gmf.FindEncoder(gmf.AV_CODEC_ID_MPEG1VIDEO)
	if err != nil {
		fatal(err)
	}

	videoEncCtx := gmf.NewCodecCtx(codec)
	if videoEncCtx == nil {
		fatal(err)
	}
	defer gmf.Release(videoEncCtx)

	outputCtx, err := gmf.NewOutputCtx(outputfilename)
	if err != nil {
		fatal(err)
	}

	videoEncCtx.
		SetBitRate(400000).
		SetWidth(dstWidth).
		SetHeight(dstHeight).
		SetTimeBase(gmf.AVR{1, 25}).
		SetPixFmt(gmf.AV_PIX_FMT_YUV420P).
		SetProfile(gmf.FF_PROFILE_MPEG4_SIMPLE).
		SetMbDecision(gmf.FF_MB_DECISION_RD)

	if outputCtx.IsGlobalHeader() {
		videoEncCtx.SetFlag(gmf.CODEC_FLAG_GLOBAL_HEADER)
	}

	videoStream := outputCtx.NewStream(codec)
	if videoStream == nil {
		fatal(errors.New(fmt.Sprintf("Unable to create stream for videoEnc [%s]\n", codec.LongName())))
	}
	defer gmf.Release(videoStream)

	if err := videoEncCtx.Open(nil); err != nil {
		fatal(err)
	}

	videoStream.SetCodecCtx(videoEncCtx)

	outputCtx.SetStartTime(0)

	if err := outputCtx.WriteHeader(); err != nil {
		fatal(err)
	}

	var frame *gmf.Frame
	i := int64(0)

	for frame = range gmf.GenSyntVideoNewFrame(videoEncCtx.Width(), videoEncCtx.Height(), videoEncCtx.PixFmt()) {
		frame.SetPts(i)

		if p, ready, err := frame.EncodeNewPacket(videoStream.CodecCtx()); ready {
			if p.Pts() != gmf.AV_NOPTS_VALUE {
				p.SetPts(gmf.RescaleQ(p.Pts(), videoStream.CodecCtx().TimeBase(), videoStream.TimeBase()))
			}

			if p.Dts() != gmf.AV_NOPTS_VALUE {
				p.SetDts(gmf.RescaleQ(p.Dts(), videoStream.CodecCtx().TimeBase(), videoStream.TimeBase()))
			}

			if err := outputCtx.WritePacket(p); err != nil {
				fatal(err)
			}

			log.Printf("Write frame=%d size=%v pts=%v dts=%v\n", frame.Pts(), p.Size(), p.Pts(), p.Dts())

			gmf.Release(p)

		} else if err != nil {
			fatal(err)
		} else {
			log.Printf("Write frame=%d frame=%d is not ready", i, frame.Pts())
		}

		i++
		gmf.Release(frame)
	}

	outputCtx.CloseOutputAndRelease()

	log.Println(i, "frames written to", outputfilename)
}
