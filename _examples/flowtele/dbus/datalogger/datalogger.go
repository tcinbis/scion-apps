package datalogger

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
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
	writerChan chan channelData
	abortChan  chan struct{}
	csvFile    *os.File
	writer     *csv.Writer
	metaData   []string
	header     []string
	wg         *sync.WaitGroup
}

func NewDbusDataLogger(csvFileName string, csvHeader []string, metaDataHeader []string, waitGroup *sync.WaitGroup) *DbusDataLogger {
	file, err := os.Create(csvFileName)
	check(err)
	d := DbusDataLogger{
		writerChan: make(chan channelData, 100),
		abortChan:  make(chan struct{}, 1),
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
	close(d.abortChan)
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
			case <-d.abortChan:
				fmt.Println("CSV Writer received abort. Flushing file")
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
