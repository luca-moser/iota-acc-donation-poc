package main

import (
	"encoding/json"
	"fmt"
	"github.com/beevik/ntp"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/store"
	"github.com/iotaledger/iota.go/api"
	"github.com/iotaledger/iota.go/consts"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

const configFile = "wallet.json"
const dateFormat = "2006-02-01 15:04:05"

type NTPClock struct {
	server string
}

func (clock *NTPClock) Now() (time.Time, error) {
	t, err := ntp.Time(clock.server)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "NTP clock error")
	}
	return t.UTC(), nil
}

type config struct {
	Seed                       string   `json:"seed"`
	PrimaryNode                string   `json:"primary_node"`
	QuorumNodes                []string `json:"quorum_nodes"`
	QuorumThreshold            float64  `json:"quorum_threshold"`
	NoResponseTolerance        float64  `json:"no_response_tolerance"`
	DataDir                    string   `json:"data_dir"`
	MWM                        uint64   `json:"mwm"`
	GTTADepth                  uint64   `json:"gtta_depth"`
	SecurityLevel              uint64   `json:"security_level"`
	TransferPollInterval       uint64   `json:"transfer_poll_interval"`
	PromoteReattachInterval    uint64   `json:"promote_reattach_interval"`
	AddressValidityTimeoutDays uint64   `json:"address_validity_timeout_days"`
	NTPServer                  string   `json:"ntp_server"`
}

func readConfig() *config {
	configBytes, err := ioutil.ReadFile(configFile)
	must(err)

	config := &config{}
	must(json.Unmarshal(configBytes, config))
	return config
}

func main() {
	conf := readConfig()

	// compose quorum API
	httpClient := &http.Client{Timeout: time.Duration(5) * time.Second}
	iotaAPI, err := api.ComposeAPI(api.QuorumHTTPClientSettings{
		PrimaryNode:         &conf.PrimaryNode,
		Threshold:           conf.QuorumThreshold,
		NoResponseTolerance: conf.NoResponseTolerance,
		Client:              httpClient,
		Nodes:               conf.QuorumNodes,
	}, api.NewQuorumHTTPClient)
	must(err)

	// make sure data dir exists
	os.MkdirAll(conf.DataDir, os.ModePerm)

	// init store for the account
	badger, err := store.NewBadgerStore(conf.DataDir)
	must(err)

	// init NTP time source
	ntpClock := &NTPClock{conf.NTPServer}

	// init account
	acc, err := account.NewAccount(conf.Seed, badger, iotaAPI, &account.AccountsOpts{
		MWM: conf.MWM, Depth: conf.GTTADepth, SecurityLevel: consts.SecurityLevel(conf.SecurityLevel),
		PromoteReattachInterval: conf.PromoteReattachInterval, TransferPollInterval: conf.TransferPollInterval,
		Clock: ntpClock,
	})
	must(err)
	defer acc.Shutdown()

	// generate an own deposit address which expires in 3 days
	// (just for having an address to send funds to for initial funding)
	now, err := ntpClock.Now()
	must(err)
	now = now.AddDate(0, 0, 3)
	depCond, err := acc.NewDepositRequest(&deposit.Request{TimeoutOn: &now})
	must(err)
	fmt.Println("own address", depCond.Address)

	// print the current usable balance
	balance, err := acc.UsableBalance()
	must(err)
	fmt.Println("current balance", balance, "iotas")

	// log all events happening around the account
	logAccountEvents(acc)

	// wait for magnet-link input
	for {
		fmt.Println("Enter magnet-link:")
		var rawLink string
		if _, err := fmt.Scanln(&rawLink); err != nil {
			continue
		}

		// parse the magnet link
		conds, err := deposit.ParseMagnetLink(rawLink)
		if err != nil {
			fmt.Println("invalid magnet link supplied:", err.Error())
			continue
		}

		// get the current time from the NTP server
		currentTime, err := ntpClock.Now()
		must(err)

		// check whether the magnet link expired
		if currentTime.After(*conds.TimeoutOn) {
			fmt.Printf("the magnet-link is expired on %s, (it's currently %s)\n", conds.TimeoutOn.Format(dateFormat), currentTime.Format(dateFormat))
			continue
		}

		// check whether we are still in time for doing this transfer
		if currentTime.Add(time.Duration(24) * time.Hour).After(*conds.TimeoutOn) {
			fmt.Println("the magnet link expires in less than 24 hours, please request a new one")
			continue
		}

		// send the transfer
		fmt.Println("sending", 10, "iotas to", conds.Address)
		recipient := account.Recipient{Address: conds.Address, Value: 10}
		bndl, err := acc.Send(recipient)
		switch err {
		case consts.ErrInsufficientBalance:
			fmt.Println("insufficient funds for doing the transfer (!)")
		case nil:
		default:
			must(err)
		}

		fmt.Println("sent bundle with tail tx", bndl[0].Hash)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func logAccountEvents(acc *account.Account) {

	// create a new listener which listens on the given account events
	listener := account.ComposeEventListener(acc, account.EventPromotion, account.EventReattachment,
		account.EventSendingTransfer, account.EventTransferConfirmed, account.EventReceivedDeposit,
		account.EventReceivingDeposit, account.EventReceivedMessage)

	go func() {
		for {
			select {
			case ev := <-listener.Promotions:
				fmt.Printf("promoted %s with %s\n", ev.BundleHash[:10], ev.PromotionTailTxHash)
			case ev := <-listener.Reattachments:
				fmt.Printf("reattached %s with %s\n", ev.BundleHash[:10], ev.ReattachmentTailTxHash)
			case ev := <-listener.Sending:
				tail := ev[0]
				fmt.Printf("sending %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.Sent:
				tail := ev[0]
				fmt.Printf("send (confirmed) %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.ReceivingDeposit:
				tail := ev[0]
				fmt.Printf("receiving deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.ReceivedDeposit:
				tail := ev[0]
				fmt.Printf("received deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.ReceivedMessage:
				tail := ev[0]
				fmt.Printf("received msg %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case errorEvent := <-acc.Errors():
				if errorEvent.Type == account.ErrorPromoteTransfer || errorEvent.Type == account.ErrorReattachTransfer {
					continue
				}
				fmt.Printf("received error: %s\n", errorEvent.Error)
			}
		}
	}()
}
