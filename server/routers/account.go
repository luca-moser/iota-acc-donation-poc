package routers

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/iotaledger/iota.go/account"
	"github.com/labstack/echo"
	"github.com/luca-moser/donapoc/server/controllers"
	"net/http"
	"sync"
	"time"
)

type AccRouter struct {
	WebEngine *echo.Echo           `inject:""`
	Dev       bool                 `inject:"dev"`
	AccCtrl   *controllers.AccCtrl `inject:""`
}

type balance struct {
	Balance uint64 `json:"balance"`
}

type MsgType byte

const (
	MsgStop = iota // this is an actual keyword
	MsgPromotion
	MsgReattachment
	MsgSending
	MsgSent
	MsgReceivingDeposit
	MsgReceivedDeposit
	MsgReceivedMessage
	MsgError
	MsgBalance
)

type wsmsg struct {
	MsgType MsgType     `json:"msg_type"`
	Data    interface{} `json:"data"`
	TS      time.Time   `json:"ts"`
}

var (
	upgrader = websocket.Upgrader{}
)

type errormsg struct {
	Error string            `json:"error"`
	Type  account.ErrorType `json:"type"`
}

func (accRouter *AccRouter) Init() {

	acc := accRouter.AccCtrl.Acc
	g := accRouter.WebEngine.Group("/account")

	// register an event listener for the given events
	listener := account.ComposeEventListener(acc, account.EventPromotion, account.EventReattachment,
		account.EventSendingTransfer, account.EventTransferConfirmed, account.EventReceivedDeposit,
		account.EventReceivingDeposit, account.EventReceivedMessage)

	// hold on to connected websocket clients
	wsMu := sync.Mutex{}
	var nextWsId int
	wses := map[int]*websocket.Conn{}

	sendWsMsg := func(data *wsmsg) {
		data.TS = time.Now()
		wsMu.Lock()
		defer wsMu.Unlock()

		for _, v := range wses {
			if err := v.WriteJSON(data); err != nil {
				// TODO: do something
			}
		}
	}

	// send account events to connected websocket clients
	go func() {
		for {
			var msg *wsmsg
			select {
			case ev := <-listener.Promotions:
				msg = &wsmsg{MsgType: MsgPromotion, Data: ev}
			case ev := <-listener.Reattachments:
				msg = &wsmsg{MsgType: MsgReattachment, Data: ev}
			case ev := <-listener.Sending:
				msg = &wsmsg{MsgType: MsgSending, Data: ev}
			case ev := <-listener.Sent:
				msg = &wsmsg{MsgType: MsgSent, Data: ev}
			case ev := <-listener.ReceivingDeposit:
				msg = &wsmsg{MsgType: MsgReceivingDeposit, Data: ev}
			case ev := <-listener.ReceivedDeposit:
				b, err := acc.Balance()
				if err == nil {
					sendWsMsg(&wsmsg{MsgType: MsgBalance, Data: b})
				}
				msg = &wsmsg{MsgType: MsgReceivedDeposit, Data: ev}
			case ev := <-listener.ReceivedMessage:
				msg = &wsmsg{MsgType: MsgReceivedMessage, Data: ev}
			case errorEvent := <-acc.Errors():
				fmt.Println(errorEvent.Error)
				fmt.Println(errorEvent.Type)
				msg = &wsmsg{MsgType: MsgError, Data: errormsg{errorEvent.Error.Error(), errorEvent.Type}}
			}

			sendWsMsg(msg)
		}
	}()

	g.GET("/donation-link", func(c echo.Context) error {
		conditions, err := accRouter.AccCtrl.GenerateNewDonationAddress()
		if err != nil {
			sendWsMsg(&wsmsg{MsgType: MsgError, Data: err})
			return err
		}
		return c.JSON(http.StatusOK, *conditions)
	})

	g.GET("/balance", func(c echo.Context) error {
		b, err := acc.Balance()
		if err != nil {
			sendWsMsg(&wsmsg{MsgType: MsgError, Data: err})
			return err
		}
		return c.JSON(http.StatusOK, balance{Balance: b})
	})

	g.GET("/live", func(c echo.Context) error {
		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}

		// register new websocket connection
		var thisID int
		wsMu.Lock()
		nextWsId++
		thisID = nextWsId
		wses[nextWsId] = ws
		wsMu.Unlock()

		// cleanup up on disconnect
		defer func() {
			wsMu.Lock()
			delete(wses, thisID)
			wsMu.Unlock()
			ws.Close()
		}()

		// loop infinitely
		for {
			msg := &wsmsg{}
			if err := ws.ReadJSON(msg); err != nil {
				break
			}

			switch msg.MsgType {
			case MsgStop:
				break
			}
		}

		return nil
	})
}
