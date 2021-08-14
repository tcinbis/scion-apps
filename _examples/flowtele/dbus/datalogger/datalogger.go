package datalogger

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/netsec-ethz/scion-apps/_examples/flowtele/utils"
	"github.com/scionproto/scion/go/lib/addr"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scionproto/scion/go/lib/log"
)

type channelData []string

type ChannelDataStruct interface {
	Strings() []string
}

type RTTData struct {
	FlowID    string
	Timestamp time.Time
	SRtt      time.Duration
}

type CwndData struct {
	FlowID    string
	Timestamp time.Time
	Cwnd      uint64
}

type LostRatioData struct {
	FlowID    string
	Timestamp time.Time
	LostRatio float64
}

type DbusDataLogger struct {
	ctx        context.Context
	writerChan chan channelData
	csvFile    *os.File
	writer     *csv.Writer
	metaData   []string
	header     []string
	wg         *sync.WaitGroup
}

func NewDbusDataLogger(ctx context.Context, csvFileName string, csvHeader []string, metaDataHeader []string, waitGroup *sync.WaitGroup) *DbusDataLogger {
	file, err := os.Create(csvFileName)
	check(err)
	d := DbusDataLogger{
		ctx:        ctx,
		writerChan: make(chan channelData, 100),
		csvFile:    file,
		header:     append(csvHeader, metaDataHeader...),
		wg:         waitGroup,
	}
	d.writer = csv.NewWriter(d.csvFile)
	d.writeHeader()

	return &d
}

func (d *DbusDataLogger) SetMetadata(meta []string) {
	d.metaData = meta
}

func (d *DbusDataLogger) Send(data ChannelDataStruct) {
	d.writerChan <- data.Strings()
}

func (d *DbusDataLogger) SendString(data []string) {
	d.writerChan <- data
}

func (d *DbusDataLogger) writeHeader() {
	check(d.writer.Write(d.header))
	check(d.writer.Error())
}

func (d *DbusDataLogger) Close() {
	close(d.writerChan)
}

func (d *DbusDataLogger) Run() {
	d.wg.Add(1)
	go func() {
		defer log.HandlePanic()
		defer func() {
			fmt.Println("### Closing csvFile ###")
			check(d.csvFile.Close())
			check(d.writer.Error())
			d.wg.Done()
		}()
		i := 1
	loop:
		for {
			select {
			case <-d.ctx.Done():
				d.Close()
				fmt.Println("CSV Writer ctx Done. Flushing file")
				for s := range d.writerChan {
					check(d.writer.Write(append(s, d.metaData...)))
					check(d.writer.Error())
				}
				d.writer.Flush()
				check(d.writer.Error())
				fmt.Printf("%d left in writter channel\n", len(d.writerChan))
				break loop
			case s := <-d.writerChan:
				if len(s) > 0 {
					check(d.writer.Write(append(s, d.metaData...)))
					check(d.writer.Error())
					i++
				}

				if i%500 == 0 {
					d.writer.Flush()
					check(d.writer.Error())
				}
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		fmt.Println("### Exiting datalogger run function ###")
	}()
}

func (r *RTTData) Strings() []string {
	return []string{r.FlowID, strconv.Itoa(int(UnixMicroseconds(r.Timestamp))), strconv.Itoa(int(r.SRtt.Microseconds()))}
}

func (c *CwndData) Strings() []string {
	return []string{c.FlowID, strconv.Itoa(int(UnixMicroseconds(c.Timestamp))), strconv.FormatUint(c.Cwnd, 10)}
}

func (c *LostRatioData) Strings() []string {
	return []string{c.FlowID, strconv.Itoa(int(UnixMicroseconds(c.Timestamp))), strconv.FormatFloat(c.LostRatio, 'f', 5, 64)}
}

func UnixMicroseconds(t time.Time) int64 {
	return t.UnixNano() / 1e3
}

func check(err error) {
	if err != nil {
		fmt.Printf("Error in dbus datalogger: %v\n", err)
		panic(err)
	}
}

func CreateDataLoggers(ctx context.Context, useScion bool, csvPrefix string, waitGroup *sync.WaitGroup, localIA, remoteIA addr.IA, localAddr, remoteAddr *net.UDPAddr, connID string) (*DbusDataLogger, *DbusDataLogger, *DbusDataLogger) {
	log.Info(fmt.Sprintf("Configuring data logger now..."))
	var metadataHeader []string
	if useScion {
		metadataHeader = []string{"localIA", "remoteIA", "src", "dest"}
	} else {
		metadataHeader = []string{"src", "dest"}
	}

	srttLogger := NewDbusDataLogger(
		ctx,
		fmt.Sprintf(
			"%s-samples-%d-%s-f%s.csv", csvPrefix, time.Now().Unix(), utils.CleanStringForFS(remoteAddr.String()), connID,
		),
		[]string{"flowID", "microTimestamp", "microSRTT"},
		metadataHeader,
		waitGroup,
	)

	lostLogger := NewDbusDataLogger(
		ctx,
		fmt.Sprintf(
			"lostRatios-%d-%s-f%s.csv", time.Now().Unix(), utils.CleanStringForFS(remoteAddr.String()), connID,
		),
		[]string{"flowID", "microTimestamp", "lostRatio"},
		metadataHeader,
		waitGroup,
	)

	cwndLogger := NewDbusDataLogger(
		ctx,
		fmt.Sprintf(
			"cwnd-samples-%d-%s-f%s.csv", time.Now().Unix(), utils.CleanStringForFS(remoteAddr.String()), connID,
		),
		[]string{"flowID", "microTimestamp", "cwnd"},
		metadataHeader,
		waitGroup,
	)

	var meta []string
	if useScion {
		meta = append([]string{localIA.String(), remoteIA.String(), localAddr.String()}, strings.Split(remoteAddr.String(), ",")...)
	} else {
		meta = append([]string{localAddr.String()}, strings.Split(remoteAddr.String(), ",")...)
	}

	srttLogger.SetMetadata(meta)
	lostLogger.SetMetadata(meta)
	cwndLogger.SetMetadata(meta)

	srttLogger.Run()
	lostLogger.Run()
	cwndLogger.Run()

	log.Info(fmt.Sprintf("Data logger complete"))

	return srttLogger, lostLogger, cwndLogger
}
