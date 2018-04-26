package main

import (
	"log"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/tuan3w/gmf"
)

func fatal(err error) {
	debug.PrintStack()
	log.Fatal(err)
}

func assert(i interface{}, err error) interface{} {
	if err != nil {
		fatal(err)
	}

	return i
}

var i int = 0

func writeFile(b []byte) {
	name := "./tmp/" + strconv.Itoa(i) + ".jpg"

	fp, err := os.Create(name)
	if err != nil {
		fatal(err)
	}

	defer func() {
		if err := fp.Close(); err != nil {
			fatal(err)
		}
		i++
	}()

	if n, err := fp.Write(b); err != nil {
		fatal(err)
	} else {
		log.Println(n, "bytes written to", name)
	}
}

func main() {
	srcFileName := "tests-sample.mp4"

	os.Mkdir("./tmp", 0755)

	if len(os.Args) > 1 {
		srcFileName = os.Args[1]
	}

	inputCtx := assert(gmf.NewInputCtx(srcFileName)).(*gmf.FmtCtx)
	defer inputCtx.CloseInputAndRelease()

	srcVideoStream, err := inputCtx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)
	if err != nil {
		log.Println("No video stream found in", srcFileName)
	}

	codec, err := gmf.FindEncoder(gmf.AV_CODEC_ID_JPEG2000)
	if err != nil {
		fatal(err)
	}

	cc := gmf.NewCodecCtx(codec)
	defer gmf.Release(cc)

	cc.SetPixFmt(gmf.AV_PIX_FMT_RGB24).SetWidth(srcVideoStream.CodecCtx().Width()).SetHeight(srcVideoStream.CodecCtx().Height())
	cc.SetTimeBase(srcVideoStream.CodecCtx().TimeBase().AVR())

	if codec.IsExperimental() {
		cc.SetStrictCompliance(gmf.FF_COMPLIANCE_EXPERIMENTAL)
	}

	if err := cc.Open(nil); err != nil {
		fatal(err)
	}

	swsCtx := gmf.NewSwsCtx(srcVideoStream.CodecCtx(), cc, gmf.SWS_BICUBIC)
	defer gmf.Release(swsCtx)

	dstFrame := gmf.NewFrame().
		SetWidth(srcVideoStream.CodecCtx().Width()).
		SetHeight(srcVideoStream.CodecCtx().Height()).
		SetFormat(gmf.AV_PIX_FMT_RGB24)
	defer gmf.Release(dstFrame)

	if err := dstFrame.ImgAlloc(); err != nil {
		fatal(err)
	}

	for packet := range inputCtx.GetNewPackets() {
		if packet.StreamIndex() != srcVideoStream.Index() {
			// skip non video streams
			continue
		}
		ist := assert(inputCtx.GetStream(packet.StreamIndex())).(*gmf.Stream)

		for frame := range packet.Frames(ist.CodecCtx()) {
			swsCtx.Scale(frame, dstFrame)

			if p, ready, _ := dstFrame.EncodeNewPacket(cc); ready {
				writeFile(p.Data())
				defer gmf.Release(p)
			}
		}
		gmf.Release(packet)
	}

	gmf.Release(dstFrame)

}
