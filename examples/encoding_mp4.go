/*
Copyright (c) 2015, EMSYM Corporation

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

    * Redistributions of source code must retain the above copyright notice,
      this list of conditions and the following disclaimer.
    * Redistributions in binary form must reproduce the above copyright notice,
      this list of conditions and the following disclaimer in the documentation
      and/or other materials provided with the distribution.
    * Neither the name of EMSYM Corporation nor the names of its contributors
      may be used to endorse or promote products derived from this software
      without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING,
BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR
OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF
THE POSSIBILITY OF SUCH DAMAGE.

Sleepy Programmer <hunan@emsym.com>

*/

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

func audio(outputCtx *gmf.FmtCtx, output chan *gmf.Packet) *gmf.Stream {
	mic, _ := gmf.NewInputCtxWithFormatName("sine=b=4", "lavfi")
	mic.Dump()

	ast, err := mic.GetBestStream(gmf.AVMEDIA_TYPE_AUDIO)
	if err != nil {
		log.Fatal("failed to find audio stream")
	}
	cc := ast.CodecCtx()

	/// fifo
	fifo := gmf.NewAVAudioFifo(cc.SampleFmt(), cc.Channels(), 256)
	if fifo == nil {
		log.Fatal("failed to create audio fifo")
	}

	/// output
	codec, err := gmf.FindEncoder("libmp3lame")
	if err != nil {
		log.Fatal("find encoder error:", err.Error())
	}

	occ := gmf.NewCodecCtx(codec)
	if occ == nil {
		log.Fatal("new output codec context error:", err.Error())
	}

	occ.SetSampleFmt(gmf.AV_SAMPLE_FMT_S16P).
		SetSampleRate(cc.SampleRate()).
		SetBitRate(128e3)
	channelLayout := occ.SelectChannelLayout()
	occ.SetChannelLayout(channelLayout)

	if err := occ.Open(nil); err != nil {
		log.Fatal("can't open output codec context", err.Error())
		return nil
	}

	/// resample
	options := []*gmf.Option{
		{"in_channel_layout", cc.ChannelLayout()},
		{"out_channel_layout", occ.ChannelLayout()},
		{"in_sample_rate", cc.SampleRate()},
		{"out_sample_rate", occ.SampleRate()},
		{"in_sample_fmt", gmf.SampleFmt(cc.SampleFmt())},
		{"out_sample_fmt", gmf.SampleFmt(gmf.AV_SAMPLE_FMT_S16P)},
	}

	swrCtx := gmf.NewSwrCtx(options, occ)
	if swrCtx == nil {
		log.Fatal("unable to create Swr Context")
	}

	if outputCtx.IsGlobalHeader() {
		occ.SetFlag(gmf.CODEC_FLAG_GLOBAL_HEADER)
	}

	ost := outputCtx.NewStream(codec)
	if ost == nil {
		log.Fatal("Unable to create stream for [%s]\n", codec.LongName())
	}

	ost.SetCodecCtx(occ)

	go func() {
		count := int64(0)
		for packet := range mic.GetNewPackets() {
			srcFrame, got, ret, err := packet.DecodeToNewFrame(ast.CodecCtx())
			gmf.Release(packet)
			if !got || ret < 0 || err != nil {
				log.Println("input audio error:", err)
				continue
			}

			fifo.Write(srcFrame)

			for fifo.SamplesToRead() >= 1152 {
				winFrame := fifo.Read(1152)
				dstFrame := swrCtx.Convert(winFrame)
				gmf.Release(winFrame)

				if dstFrame == nil {
					continue
				}
				count += 1152

				dstFrame.SetPts(count)

				writePacket, ready, _ := dstFrame.EncodeNewPacket(occ)
				if ready {
					writePacket.SetStreamIndex(ost.Index())
					output <- writePacket
				}
				gmf.Release(dstFrame)
			}
			gmf.Release(srcFrame)
		}
	}()
	return ost
}
func video(outputCtx *gmf.FmtCtx, output chan *gmf.Packet) *gmf.Stream {
	in, err := gmf.NewInputCtxWithFormatName("testsrc=decimals=3", "lavfi")
	ist, err := in.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)
	if err != nil {
		log.Fatal("Can't open test video source")
	}

	ist.CodecCtx().PixFmt()

	codec, err := gmf.FindEncoder(gmf.AV_CODEC_ID_MPEG4)
	if err != nil {
		fatal(err)
	}
	videoEncCtx := gmf.NewCodecCtx(codec)
	if videoEncCtx == nil {
		fatal(err)
	}

	videoEncCtx.
		SetBitRate(1e6).
		SetWidth(ist.CodecCtx().Width()).
		SetHeight(ist.CodecCtx().Height()).
		SetTimeBase(ist.TimeBase().AVR()).
		SetPixFmt(gmf.AV_PIX_FMT_YUV420P).
		SetMbDecision(gmf.FF_MB_DECISION_RD)

	if outputCtx.IsGlobalHeader() {
		videoEncCtx.SetFlag(gmf.CODEC_FLAG_GLOBAL_HEADER)
	}

	videoStream := outputCtx.NewStream(codec)
	if videoStream == nil {
		fatal(errors.New(fmt.Sprintf("Unable to create stream for videoEnc [%s]\n", codec.LongName())))
	}
	if err := videoEncCtx.Open(nil); err != nil {
		fatal(err)
	}

	videoStream.SetCodecCtx(videoEncCtx)

	swsCtx := gmf.NewSwsCtx(ist.CodecCtx(), videoEncCtx, gmf.SWS_BICUBIC)

	dstFrame := gmf.NewFrame().
		SetWidth(ist.CodecCtx().Width()).
		SetHeight(ist.CodecCtx().Height()).
		SetFormat(gmf.AV_PIX_FMT_YUV420P)

	if err := dstFrame.ImgAlloc(); err != nil {
		fatal(err)
	}

	go func() {
		i := int64(0)

		for packet := range in.GetNewPackets() {
			if packet.StreamIndex() != ist.Index() {
				// skip non video streams
				continue
			}

			for frame := range packet.Frames(ist.CodecCtx()) {
				swsCtx.Scale(frame, dstFrame)
				dstFrame.SetPts(i)

				if p, ready, err := dstFrame.EncodeNewPacket(videoEncCtx); ready {
					p.SetStreamIndex(videoStream.Index())
					if p.Pts() != gmf.AV_NOPTS_VALUE {
						p.SetPts(gmf.RescaleQ(p.Pts(), videoEncCtx.TimeBase(), videoStream.TimeBase()))
					}

					if p.Dts() != gmf.AV_NOPTS_VALUE {
						p.SetDts(gmf.RescaleQ(p.Dts(), videoEncCtx.TimeBase(), videoStream.TimeBase()))
					}
					output <- p
				} else if err != nil {
					fatal(err)
				} else {
					log.Printf("encode frame=%d frame=%d is not ready", i, frame.Pts())
				}

				i++
			}
		}
		close(output)
	}()
	return videoStream
}
func main() {
	outputfilename := "sample-encoding.mp4"

	outputCtx, err := gmf.NewOutputCtx(outputfilename)
	if err != nil {
		fatal(err)
	}

	achan := make(chan *gmf.Packet)
	ast := audio(outputCtx, achan)

	vchan := make(chan *gmf.Packet)
	vst := video(outputCtx, vchan)

	outputCtx.SetStartTime(0)

	if err := outputCtx.WriteHeader(); err != nil {
		fatal(err)
	}

	var vp *gmf.Packet
	i := 0
	for ap := range achan {
		for {
			if vp == nil {
				vp = <-vchan
			}

			if gmf.CompareTimeStamp(int(vp.Pts()), vst.TimeBase(), int(ap.Pts()), ast.TimeBase()) <= 0 {
				if err := outputCtx.WritePacket(vp); err != nil {
					fatal(err)
				}
				gmf.Release(vp)
				vp = nil
				i++
				continue
			} else {
				if err := outputCtx.WritePacket(ap); err != nil {
					fatal(err)
				}
				gmf.Release(ap)
			}
			break
		}
		if i > 200 {
			break
		}
	}

	outputCtx.CloseOutputAndRelease()
}
