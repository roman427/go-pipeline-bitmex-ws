package workers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/panjf2000/ants"
	"nhooyr.io/websocket"
)

var (
	tableInstrument    = []byte(`{"table":"instrument"`)
	tableTrade         = []byte(`{"table":"trade"`)
	tableOrderBookL225 = []byte(`{"table":"orderBookL2_25"`)
	symbolXBTUSD       = []byte(`"symbol":"XBTUSD"`)
)

type BitmexMsg struct {
	Table  string `json:"table"`
	Action string `json:"action"`
}

type BitmexInstrumentMsg struct {
	BitmexMsg
	Data []struct {
		Symbol    string  `json:"symbol"`
		LastPrice float32 `json:"lastPrice"`
		HighPrice float32 `json:"highPrice"`
		LowPrice  float32 `json:"lowPrice"`
		BidPrice  float32 `json:"bidPrice"`
		MidPrice  float32 `json:"midPrice"`
		AskPrice  float32 `json:"askPrice"`
		MarkPrice float32 `json:"markPrice"`

		LastPriceProtected float32   `json:"lastPriceProtected"`
		LastTickDirection  string    `json:"lastTickDirection"`
		LastChangePcnt     float32   `json:"lastChangePcnt"`
		OpenInterest       int64     `json:"openInterest"`
		OpenValue          int64     `json:"openValue"`
		Timestamp          time.Time `json:"timestamp"`
	} `json:"data"`
}
type BitmexOrderBookL225Msg struct {
	BitmexMsg
}
type BitmexTradeMsg struct {
	BitmexMsg
}

type BitmexConfig struct {
	Url    string
	Key    string
	Secret string
}

type BitmexWorker struct {
	mtx                  sync.RWMutex
	apiKey               string
	apiSecret            string
	url                  string
	maxReconnectAttempts int

	pool              *ants.Pool
	conn              *websocket.Conn
	status            int
	reconnectAttempts int

	onConnect    func()
	onDisconnect func()
	onMessage    func([]byte)
	onError      func()

	instruments map[string]string

	BaseWorkers
}

func NewBitmexWorker(ctx context.Context, bcfg *BitmexConfig, maxReconnectAttempts int) (*BitmexWorker, error) {
	var err error

	bw := &BitmexWorker{
		url:                  bcfg.Url,
		apiKey:               bcfg.Key,
		apiSecret:            bcfg.Secret,
		maxReconnectAttempts: maxReconnectAttempts,
		instruments:          make(map[string]string),
	}

	bw.ctx, bw.cancel = context.WithCancel(ctx)

	if bw.pool, err = ants.NewPool(100, ants.WithPreAlloc(true)); err != nil {
		return nil, err
	}

	bw.onConnect = bw.ctx.Value("onConnect").(func())
	bw.onDisconnect = bw.ctx.Value("onDisconnect").(func())
	bw.onMessage = bw.ctx.Value("onMessage").(func([]byte))
	bw.onError = bw.ctx.Value("onError").(func())

	return bw, nil
}

func (bw *BitmexWorker) generateBitmexSignature(secret, verb, url string, nonce int64, data []byte) string {
	var sign string
	var buffer bytes.Buffer
	// buffer.WriteString(secret)
	buffer.WriteString(verb)
	buffer.WriteString(url)
	buffer.WriteString(strconv.FormatInt(nonce, 10))
	buffer.Write(data)
	// # signature = HEX(HMAC_SHA256(secret, 'POST/api/v1/order1416993995705{"symbol":"XBTZ14","quantity":1,"price":395.01}'))
	hash := hmac.New(sha256.New, []byte(secret))
	hash.Write(buffer.Bytes())
	sign = hex.EncodeToString(hash.Sum(nil))
	// sign = base64.StdEncoding.EncodeToString(hash.Sum(nil))

	return sign
}

func (bw *BitmexWorker) authKey(apiKey, sign string, nonce int64) []byte {
	// {"op": "authKey", "args": ["ZKr5x2qfPXgHyIK-C-6lBLyJ", 1514597637401, "810ec79e41ad347dc7ee5bb00fbe11cb5bb0d6bd11e0342a4b283ff6a802308d"]}
	var buffer bytes.Buffer
	buffer.WriteString("{\"op\": \"authKey\", \"args\": [\"")
	buffer.WriteString(apiKey)
	buffer.WriteString("\", ")
	buffer.WriteString(strconv.FormatInt(nonce, 10))
	buffer.WriteString(", \"")
	buffer.WriteString(sign)
	buffer.WriteString("\"]}")
	return buffer.Bytes()
}

func (bw *BitmexWorker) connect(isReconnect bool) error {
	log := bw.getlog()
	log.Info("BitmexWorker.connect")

	bw.mtx.Lock()
	if bw.status == CONNECTED || bw.status == RECONNECTING || bw.status == CONNECTING {
		bw.mtx.Unlock()
		return nil
	}

	if isReconnect {
		bw.status = RECONNECTING
	} else {
		bw.status = CONNECTING
	}
	bw.mtx.Unlock()

	var err error
	header := http.Header{}
	opt := &websocket.DialOptions{}

	opt.HTTPHeader = header

	nonce := time.Now().UnixNano() / 1000000
	sign := bw.generateBitmexSignature(bw.apiSecret, "GET", "/realtime", nonce, []byte(""))
	conn, _, err := websocket.Dial(bw.ctx, bw.url, opt)

	if err != nil {
		log.Error("BitmexWorker.connect Dial error: ", err)
		bw.handleDisconnect(true, err)
		return nil
	}

	bw.mtx.Lock()

	if bw.status == DISCONNECTED {
		bw.mtx.Unlock()
		return nil
	}

	bw.conn = conn
	bw.mtx.Unlock()

	if err := bw.send(bw.authKey(bw.apiKey, sign, nonce)); err != nil {
		log.Error("BitmexWorker.connect send logon msg error: ", err)
		bw.handleDisconnect(true, err)
		return nil
	}
	if err := bw.Subscribe(); err != nil {
		log.Error("BitmexWorker.connect Subscribe error: ", err)
		bw.handleDisconnect(true, err)
		return nil
	}
	bw.conn.SetReadLimit(maxPacketSize)

	bw.mtx.Lock()
	bw.status = CONNECTED
	bw.reconnectAttempts = 0
	bw.mtx.Unlock()

	go bw.reader()
	bw.onConnect()

	return nil
}
func (bw *BitmexWorker) Subscribe() error {
	/*
		{"op":"subscribe","args":["announcement","chat:1","connected","execution","margin","instrument","liquidation","publicNotifications","privateNotifications","order","position","orderBookL2_25:XBTUSD","trade:XBTUSD"]}
		{"op":"subscribe","args":["orderBookL2_25:XBTUSD","trade:XBTUSD"]}
	*/
	if err := bw.send([]byte(`{"op":"subscribe","args":["instrument:XBTUSD", "orderBookL2_25:XBTUSD","trade:XBTUSD"]}`)); err != nil {
		return err
	}
	return nil
}

func (bw *BitmexWorker) send(cmd []byte) error {
	writer, err := bw.conn.Writer(bw.ctx, websocket.MessageText)

	if err != nil {
		return err
	}

	if _, err := writer.Write(cmd); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}
	return nil
}

func (bw *BitmexWorker) xreader() {
	log := bw.getlog()
	for {
		_, msg, err := bw.conn.Read(bw.ctx)
		if err != nil {
			log.Error("BitmexWorker.reader conn.Read error: ", err)
		}
		bw.onMessage(msg)
		// _ = bw.pool.Submit(func() {})
	}
}

func (bw *BitmexWorker) reader() {
	log := bw.getlog()
	log.Debug("BitmexWorker.reader start: ")
	buffer := make([]byte, maxPacketSize)

	for {
		select {
		case <-bw.ctx.Done():
			return
		default:
			_, reader, err := bw.conn.Reader(bw.ctx)
			if err != nil {
				if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					log.Error("BitmexWorker.reader conn.Reader error: ", err)
					bw.onError()
				}
				bw.handleDisconnect(true, err)
				return
			}
			n, err := reader.Read(buffer)
			if err != nil && err != io.EOF {
				log.Error("BitmexWorker.reader Read(buffer) error: ", err)
				bw.handleDisconnect(true, err)
				bw.onError()
				return
			}

			if n > 0 {
				tmp := make([]byte, n)
				copy(tmp, buffer)
				_ = bw.pool.Submit(func() {
					bw.messageProcessing(tmp)
				})
			}
		}
	}
}

func (bw *BitmexWorker) messageProcessing(msg []byte) {
	if !bytes.Contains(msg, symbolXBTUSD) {
		return
	}

	log := bw.getlog()
	if bytes.HasPrefix(msg, tableInstrument) {
		// fmt.Println("Instrument XBTUSD: ", string(msg))

		// var data = BitmexInstrumentMsg{}
		var data = struct {
			BitmexMsg
			Data []map[string]interface{} `json:"data"`
		}{}

		if err := jsonProcessor.NewDecoder(bytes.NewReader(msg)).Decode(&data); err != nil {
			log.Error("Error jsonProcessor BitmexInstrumentMsg: ", err)
			return
		}
		// fmt.Println("Instrument XBTUSD: ", data.Data)

		for _, row := range data.Data {
			for name, _ := range row {
				if _, ok := bw.instruments[name]; !ok {
					fmt.Println("Instrument : ", name, row[name])
					if row[name] != nil {
						if t := reflect.TypeOf(row[name]); t.Kind() == reflect.Ptr {
							bw.instruments[name] = t.Elem().Name()
							// fmt.Println( "TypeOf 0:", t.Elem().Name())
						} else {
							bw.instruments[name] = t.Name()
							// fmt.Println( "TypeOf 1:", t.Name())
						}
					}
				}
			}
		}

	} else if bytes.HasPrefix(msg, tableOrderBookL225) {
		// fmt.Println("OrderBookL225 XBTUSD")
	} else if bytes.HasPrefix(msg, tableTrade) {
		// fmt.Println("tableTrade XBTUSD")
	} else {
		fmt.Println("Errors table msg")
		return
	}

	// bw.onMessage(msg)

	// tableInstrument = []byte(`{"table":"instrument"`)
	// tableOrderBookL225 = []byte(`{"table":"orderBookL2_25"`)
	// symbolXBTUSD = []byte(`"symbol":"XBTUSD"`)
}

func (bw *BitmexWorker) nextAttemptConnect() time.Duration {
	if bw.reconnectAttempts <= 10 {
		return 1 * time.Second
	} else if bw.reconnectAttempts >= 10 && bw.reconnectAttempts <= 30 {
		return 5 * time.Second
	} else {
		return 20 * time.Second
	}
}

func (bw *BitmexWorker) handleDisconnect(reconnect bool, err error) {
	log := bw.getlog()

	bw.mtx.Lock()
	if bw.status == CLOSED {
		bw.mtx.Unlock()
		return
	}

	log.Debug("BitmexWorker.handleDisconnect  DISCONNECTED")
	bw.status = DISCONNECTED
	bw.mtx.Unlock()

	bw.onDisconnect()

	go func() {
		if reconnect {
			select {
			case <-bw.ctx.Done():
				return
			case <-time.After(bw.nextAttemptConnect()):
				bw.mtx.Lock()
				bw.reconnectAttempts++
				log.Debug("BitmexWorker.handleDisconnect reconnect: ", bw.reconnectAttempts)
				bw.mtx.Unlock()
				_ = bw.connect(true)
			}
		}
	}()
}

func (bw *BitmexWorker) disconnect(done chan struct{}) error {
	var err error
	defer close(done)
	bw.mtx.Lock()
	defer bw.mtx.Unlock()

	select {
	case <-bw.ctx.Done():
		err = bw.conn.Close(websocket.StatusNormalClosure, "BitmexWorker disconnect")
	default:
		err = bw.conn.Close(websocket.StatusNormalClosure, "BitmexWorker disconnect")
	}
	log := bw.getlog()
	log.Debug("BitmexWorker.disconnect  DISCONNECTED")
	bw.status = DISCONNECTED
	bw.onDisconnect()

	return err
}

func (bw *BitmexWorker) Run(ctx context.Context) error {
	log := bw.getlog()
	log.Debug("BitmexWorker.Run...")
	bw.ctx, bw.cancel = context.WithCancel(ctx)
	_ = bw.connect(false)
	log.Debug("BitmexWorker.Run. connected")

	return nil
}

func (bw *BitmexWorker) Close(complete chan struct{}) error {
	defer close(complete)
	log := bw.getlog()
	log.Debug("BitmexWorker.close start")

	for key, tt := range bw.instruments {
		fmt.Println(key, tt)
	}

	if bw.status == CONNECTED {
		done := make(chan struct{})
		if err := bw.disconnect(done); err != nil {
			log.Error("BitmexWorker.Close disconnect error: ", err)
		}
		<-done
	}

	log.Debug("BitmexWorker.Close wait done")
	bw.cancel()

	bw.mtx.Lock()
	bw.status = CLOSED
	bw.mtx.Unlock()

	return nil
}
