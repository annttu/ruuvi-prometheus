package metrics

import (
	"bytes"
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	macAddress string
)


type Tag struct {
	Rssi						int     `json:"rssi"`
	Timestamp					int64   `json:"timestamp"`
	Data						string  `json:"data"`
	DataFormat					int     `json:"dataFormat"`
	Temperature					float64 `json:"temperature"`
	Humidity					float64 `json:"humidity"`
	Pressure					int     `json:"pressure"`
	AccelX						float64 `json:"accelX"`
	AccelY						float64 `json:"accelY"`
	AccelZ						float64 `json:"accelZ"`
	MovementCounter				int     `json:"movementCounter"`
	Voltage						float64 `json:"voltage"`
	TxPower						int     `json:"txPower"`
	MeasurementSequenceNumber	int     `json:"measurementSequenceNumber"`
	Id							string  `json:"id"`
}


type HistoryData struct {
	Data Gateway `json:"data"`
}


type Gateway struct {
	Coordinates 	string         `json:"coordinates"`
	Timestamp 		int64          `json:"timestamp"`
	GwMac 			string         `json:"gw_mac"`
	Tags 			map[string]Tag `json:"tags"`
}


func getMacAddress() (addr string) {
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp != 0 && bytes.Compare([]byte(iface.HardwareAddr), nil) != 0 {
				return strings.ToUpper(iface.HardwareAddr.String())
			}
		}
	}
	return "00:00:00:00:00:00"
}

func init() {
	macAddress = getMacAddress()
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI != "/history" {
		http.NotFound(w, r)
		return
	}

	data := HistoryData{
		Data: Gateway{
			Coordinates: "",
			Timestamp: time.Now().Unix(),
			GwMac: macAddress,
			Tags: make(map[string]Tag),
		},
	}

	for addr, ls := range deviceLastSeen {
		labels := prometheus.Labels{"device": addr}
		data.Data.Tags[strings.ToUpper(addr)] = Tag{
			Rssi:                      int(readGaugeVec(signalRSSI, labels)),
			Timestamp:                 ls.Unix(),
			Data:                      deviceRawData[addr],
			DataFormat:                deviceRawDataFormat[addr],
			Temperature:               readGaugeVec(temperature, labels),
			Humidity:                  readGaugeVec(humidity, labels),
			Pressure:                  int(readGaugeVec(pressure, labels)),
			AccelX:                    readGaugeVec(acceleration, prometheus.Labels{"device": addr, "axis": "X"}),
			AccelY:                    readGaugeVec(acceleration, prometheus.Labels{"device": addr, "axis": "Y"}),
			AccelZ:                    readGaugeVec(acceleration, prometheus.Labels{"device": addr, "axis": "Z"}),
			MovementCounter:           int(readGaugeVec(moveCount, labels)),
			Voltage:                   readGaugeVec(voltage, labels),
			TxPower:                   int(readGaugeVec(txPower, labels)),
			MeasurementSequenceNumber: int(readGaugeVec(seqno, labels)),
			Id: strings.ToUpper(addr),
		}
	}

	body, err := json.MarshalIndent(data, "", "    ")

	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
