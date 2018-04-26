package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"

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

var i int32 = 0

func writeFile(b []byte) {
	name := "./tmp/" + strconv.Itoa(int(atomic.AddInt32(&i, 1))) + ".jpg"

	fp, err := os.Create(name)
	if err != nil {
		fatal(err)
	}

	defer func() {
		if err := fp.Close(); err != nil {
			fatal(err)
		}
	}()

	if n, err := fp.Write(b); err != nil {
		fatal(err)
	} else {
		log.Println(n, "bytes written to", name)
	}
}

func encodeWorker(data chan *gmf.Frame, wg *sync.WaitGroup, srcCtx *gmf.CodecCtx) {
	defer wg.Done()
	log.Println("worker started")
	codec, err := FindEncoder(gmf.AV_CODEC_ID_JPEG2000)
	if err != nil {
		fatal(err)
	}

	cc := gmf.NewCodecCtx(codec)
	defer gmf.Release(cc)

	w, h := srcCtx.Width(), srcCtx.Height()

	cc.SetPixFmt(gmf.AV_PIX_FMT_RGB24).SetWidth(w).SetHeight(h)

	if codec.IsExperimental() {
		cc.SetStrictCompliance(gmf.FF_COMPLIANCE_EXPERIMENTAL)
	}

	if err := cc.Open(nil); err != nil {
		fatal(err)
	}

	swsCtx := gmf.NewSwsCtx(srcCtx, cc, gmf.SWS_BICUBIC)
	defer gmf.Release(swsCtx)

	// convert to RGB, optionally resize could be here
	dstFrame := gmf.NewFrame().
		SetWidth(w).
		SetHeight(h).
		SetFormat(gmf.AV_PIX_FMT_RGB24)
	defer gmf.Release(dstFrame)

	if err := dstFrame.ImgAlloc(); err != nil {
		fatal(err)
	}

	for {
		srcFrame, ok := <-data
		if !ok {
			break
		}
		//		log.Printf("srcFrome = %p",srcFrame)
		swsCtx.Scale(srcFrame, dstFrame)

		if p, ready, _ := dstFrame.EncodeNewPacket(cc); ready {
			writeFile(p.Data())
		}
		Release(srcFrame)
	}

}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	os.Mkdir("./tmp", 0755)

	wnum := flag.Int("wnum", 10, "number of workers")
	srcFileName := flag.String("input", "tests-sample.mp4", "input file")

	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage: %s [OPTIONS]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	inputCtx := assert(gmf.NewInputCtx(*srcFileName)).(*FmtCtx)
	defer inputCtx.CloseInputAndRelease()

	srcVideoStream, err := inputCtx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)
	if err != nil {
		log.Println("No video stream found in", srcFileName)
	}

	wg := new(sync.WaitGroup)

	dataChan := make(chan *gmf.Frame)

	for i := 0; i < *wnum; i++ {
		wg.Add(1)
		go encodeWorker(dataChan, wg, srcVideoStream.CodecCtx())
	}

	for packet := range inputCtx.GetNewPackets() {
		if packet.StreamIndex() != srcVideoStream.Index() {
			// skip non video streams
			continue
		}

		ist := assert(inputCtx.GetStream(packet.StreamIndex())).(*Stream)

		for frame := range packet.Frames(ist.CodecCtx()) {
			dataChan <- frame.CloneNewFrame()
		}
		gmf.Release(packet)
	}

	close(dataChan)

	wg.Wait()
}
