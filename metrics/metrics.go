// Copyright (c) 2018, Joonas Kuorilehto
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice, this
//    list of conditions and the following disclaimer.
//
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package metrics

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	"gitlab.com/jtaimisto/bluewalker/host"
	"gitlab.com/jtaimisto/bluewalker/ruuvi"
)

var (
	ruuviFrames = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ruuvi_frames_total",
		Help: "Total Ruuvi frames received",
	}, []string{"device"})

	humidity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_humidity_ratio",
		Help: "Ruuvi tag sensor relative humidity",
	}, []string{"device"})

	temperature = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_temperature_celsius",
		Help: "Ruuvi tag sensor temperature",
	}, []string{"device"})

	pressure = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_pressure_hpa",
		Help: "Ruuvi tag sensor air pressure",
	}, []string{"device"})

	acceleration = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_acceleration_g",
		Help: "Ruuvi tag sensor acceleration X/Y/Z",
	}, []string{"device", "axis"})

	voltage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_battery_volts",
		Help: "Ruuvi tag battery voltage",
	}, []string{"device"})

	signalRSSI = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_rssi_dbm",
		Help: "Ruuvi tag received signal strength RSSI",
	}, []string{"device"})

	format = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_format",
		Help: "Ruuvi frame format version (e.g. 3 or 5)",
	}, []string{"device"})

	txPower = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_txpower_dbm",
		Help: "Ruuvi transmit power in dBm",
	}, []string{"device"})

	moveCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_movecount_total",
		Help: "Ruuvi movement counter",
	}, []string{"device"})

	seqno = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_seqno_current",
		Help: "Ruuvi frame sequence number",
	}, []string{"device"})
)

// ttl is the duration after which sensors are forgotten if signal is lost.
const ttl = 1 * time.Minute

var deviceLastSeen map[string]time.Time
var deviceRawData map[string]string
var deviceRawDataFormat map[string]int

var mu sync.Mutex

func init() {
	deviceLastSeen = make(map[string]time.Time)
	deviceRawData = make(map[string]string)
	deviceRawDataFormat = make(map[string]int)

	go func() {
		for range time.Tick(time.Minute) {
			clearExpired()
		}
	}()
}

func ObserveRuuvi(o RuuviReading, rawData []byte) {
	addr := o.Address.String()

	if len(rawData) >= 2 && binary.LittleEndian.Uint16(rawData) == 0x0499 {
		rawData = rawData[2:]
	}

	mu.Lock()
	deviceLastSeen[addr] = time.Now()

	// FIXME: Add more robust parsing for version string
	if len(rawData) > 1 {
		deviceRawData[addr] = strings.ToUpper("0201061BFF9904" + hex.EncodeToString(rawData))
		deviceRawDataFormat[addr] = int(rawData[0])
	} else {
		deviceRawData[addr] = ""
		deviceRawDataFormat[addr] = 5
	}

	mu.Unlock()

	ruuviFrames.WithLabelValues(addr).Inc()
	signalRSSI.WithLabelValues(addr).Set(float64(o.Rssi))
	if o.VoltageValid() {
		voltage.WithLabelValues(addr).Set(float64(o.Voltage) / 1000)
	}
	if o.PressureValid() {
		pressure.WithLabelValues(addr).Set(float64(o.Pressure) / 100)
	}
	if o.TemperatureValid() {
		temperature.WithLabelValues(addr).Set(float64(o.Temperature))
	}
	if o.HumidityValid() {
		humidity.WithLabelValues(addr).Set(float64(o.Humidity) / 100)
	}
	if o.AccelerationValid() {
		acceleration.WithLabelValues(addr, "X").Set(float64(o.AccelerationX))
		acceleration.WithLabelValues(addr, "Y").Set(float64(o.AccelerationY))
		acceleration.WithLabelValues(addr, "Z").Set(float64(o.AccelerationZ))
	}
	format.WithLabelValues(addr).Set(float64(o.DataFormat()))
	if o.TxPowerValid() {
		txPower.WithLabelValues(addr).Set(float64(o.TxPower))
	}
	if o.MoveCountValid() {
		moveCount.WithLabelValues(addr).Set(float64(o.MoveCount))
	}
	if o.SeqnoValid() {
		seqno.WithLabelValues(addr).Set(float64(o.Seqno))
	}
}

func clearExpired() {
	mu.Lock()
	defer mu.Unlock()

	// log.Println("Checking for expired devices")
	now := time.Now()
	for addr, ls := range deviceLastSeen {
		if now.Sub(ls) > ttl {
			// log.Printf("%v expired", addr)
			ruuviFrames.DeleteLabelValues(addr)
			signalRSSI.DeleteLabelValues(addr)
			voltage.DeleteLabelValues(addr)
			pressure.DeleteLabelValues(addr)
			temperature.DeleteLabelValues(addr)
			humidity.DeleteLabelValues(addr)
			acceleration.DeleteLabelValues(addr, "X")
			acceleration.DeleteLabelValues(addr, "Y")
			acceleration.DeleteLabelValues(addr, "Z")
			format.DeleteLabelValues(addr)
			txPower.DeleteLabelValues(addr)
			moveCount.DeleteLabelValues(addr)
			seqno.DeleteLabelValues(addr)

			delete(deviceLastSeen, addr)
			delete(deviceRawData, addr)
		}
	}
}

type RuuviReading struct {
	*host.ScanReport
	*ruuvi.Data
}

// DataFormat guesses the Ruuvi protocol data format version. In case of
// protocol version 3, tx power, movement counter and sequence number are
// not valid. Otherwise guess version is 5.
func (r RuuviReading) DataFormat() int {
	if !r.TxPowerValid() && !r.MoveCountValid() && !r.SeqnoValid() {
		return 3
	} else {
		return 5
	}
}


func readCounterVec(m *prometheus.CounterVec, l prometheus.Labels) float64 {
	pb := &dto.Metric{}
	c := m.With(l)
	c.Write(pb)
	return pb.GetCounter().GetValue()
}

func readGaugeVec(m *prometheus.GaugeVec, l prometheus.Labels) float64 {
	pb := &dto.Metric{}
	c := m.With(l)
	c.Write(pb)
	return pb.GetGauge().GetValue()
}
