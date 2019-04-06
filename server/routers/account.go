package routers

import (
	"github.com/gorilla/websocket"
	"github.com/iotaledger/iota.go/account/event/listener"
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

type balancemsg struct {
	Usable uint64 `json:"usable"`
	Total  uint64 `json:"total"`
}

func (accRouter *AccRouter) Init() {

	acc := accRouter.AccCtrl.Acc
	eventMachine := accRouter.AccCtrl.EM
	g := accRouter.WebEngine.Group("/account")

	// register an event listener for all account events
	lis := listener.NewChannelEventListener(eventMachine).
		RegPromotions().
		RegReattachments().
		RegSentTransfers().
		RegConfirmedTransfers().
		RegReceivingDeposits().
		RegReceivedDeposits().
		RegReceivedMessages().
		RegInternalErrors()

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
			case ev := <-lis.Promoted:
				msg = &wsmsg{MsgType: MsgPromotion, Data: ev}
			case ev := <-lis.Reattached:
				msg = &wsmsg{MsgType: MsgReattachment, Data: ev}
			case ev := <-lis.SentTransfer:
				msg = &wsmsg{MsgType: MsgSending, Data: ev}
			case ev := <-lis.TransferConfirmed:
				msg = &wsmsg{MsgType: MsgSent, Data: ev}
			case ev := <-lis.ReceivingDeposit:
				msg = &wsmsg{MsgType: MsgReceivingDeposit, Data: ev}
			case ev := <-lis.ReceivedDeposit:
				usable, err := acc.AvailableBalance()
				total, err2 := acc.TotalBalance()
				if err == nil && err2 == nil {
					sendWsMsg(&wsmsg{MsgType: MsgBalance, Data: balancemsg{usable, total}})
				}
				msg = &wsmsg{MsgType: MsgReceivedDeposit, Data: ev}
			case ev := <-lis.ReceivedMessage:
				msg = &wsmsg{MsgType: MsgReceivedMessage, Data: ev}
			case err := <-lis.InternalError:
				msg = &wsmsg{MsgType: MsgError, Data: err.Error()}
			}

			sendWsMsg(msg)
		}
	}()

	g.GET("/donation-link", func(c echo.Context) error {
		conditions, err := accRouter.AccCtrl.GenerateNewDonationAddress()
		if err != nil {
			sendWsMsg(&wsmsg{MsgType: MsgError, Data: err.Error()})
			return err
		}
		return c.JSON(http.StatusOK, *conditions)
	})

	g.GET("/balance", func(c echo.Context) error {
		usable, err := acc.AvailableBalance()
		if err != nil {
			sendWsMsg(&wsmsg{MsgType: MsgError, Data: err.Error()})
			return err
		}
		total, err := acc.TotalBalance()
		if err != nil {
			sendWsMsg(&wsmsg{MsgType: MsgError, Data: err.Error()})
			return err
		}
		return c.JSON(http.StatusOK, balancemsg{usable, total})
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
