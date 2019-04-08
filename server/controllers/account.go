package controllers

import (
	"bytes"
	"encoding/gob"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/builder"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/event"
	"github.com/iotaledger/iota.go/account/plugins/transfer/poller"
	badger_store "github.com/iotaledger/iota.go/account/store/badger"
	mongo_store "github.com/iotaledger/iota.go/account/store/mongo"
	"github.com/iotaledger/iota.go/account/timesrc"
	"github.com/iotaledger/iota.go/api"
	"github.com/iotaledger/iota.go/consts"
	"github.com/luca-moser/donapoc/server/server/config"
	"github.com/luca-moser/donapoc/server/utilities"
	"github.com/pkg/errors"
	"gopkg.in/inconshreveable/log15.v2"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"
)

const currentCondsFile = "./current"

type AccCtrl struct {
	Acc         account.Account
	EM          event.EventMachine
	iota        *api.API
	store       *badger_store.BadgerStore
	Config      *config.Configuration `inject:""`
	current     *deposit.CDA
	checkCondMu sync.Mutex
	logger      log15.Logger
}

func (ac *AccCtrl) Init() error {
	logger, _ := utilities.GetLogger("acc")
	ac.logger = logger

	// register deposit condition gob
	gob.Register(deposit.CDA{})

	// read in existing deposit condition
	if _, err := os.Stat(currentCondsFile); err == nil {
		logger.Info("reading in existing deposit condition")
		currentBytes, err := ioutil.ReadFile(currentCondsFile)
		if err != nil {
			return err
		}
		dec := gob.NewDecoder(bytes.NewReader(currentBytes))
		currentCond := &deposit.CDA{}
		if err := dec.Decode(currentCond); err != nil {
			return err
		}
		ac.current = currentCond
		logger.Info("successfully read in current deposit conditions")
	}

	conf := ac.Config.App.Account

	// init quorumed (what a word) api
	quorumConf := conf.Quorum
	httpClient := &http.Client{Timeout: time.Duration(quorumConf.Timeout) * time.Second}
	a, err := api.ComposeAPI(api.QuorumHTTPClientSettings{
		PrimaryNode:                &quorumConf.PrimaryNode,
		Threshold:                  quorumConf.Threshold,
		NoResponseTolerance:        quorumConf.NoResponseTolerance,
		Client:                     httpClient,
		Nodes:                      quorumConf.Nodes,
		MaxSubtangleMilestoneDelta: quorumConf.MaxSubtangleMilestoneDelta,
	}, api.NewQuorumHTTPClient)
	if err != nil {
		return errors.Wrap(err, "unable to construct IOTA API")
	}
	ac.iota = a

	// make sure data dir exists
	mongoConf := conf.MongoDB
	dataStore, err := mongo_store.NewMongoStore(mongoConf.URI, &mongo_store.Config{
		DBName: mongoConf.DBName, CollName: mongoConf.CollName,
	})

	// init NTP time source
	ntpClock := timesrc.NewNTPTimeSource(conf.Time.NTPServer)

	// init account
	em := event.NewEventMachine()

	// init account
	b := builder.NewBuilder().
		WithAPI(a).
		WithStore(dataStore).
		WithSeed(conf.Seed).
		WithTimeSource(ntpClock).
		WithSecurityLevel(consts.SecurityLevel(conf.SecurityLevel)).
		WithMWM(conf.MWM).
		WithDepth(conf.GTTADepth).
		WithEvents(em)

	// create a poller which will check for incoming transfers
	transferPoller := poller.NewTransferPoller(
		b.Settings(), poller.NewPerTailReceiveEventFilter(true),
		time.Duration(conf.TransferPollInterval)*time.Second,
	)

	acc, err := b.Build(transferPoller)
	if err != nil {
		return errors.Wrap(err, "unable to instantiate account")
	}
	if err := acc.Start(); err != nil {
		return err
	}
	ac.Acc = acc
	ac.EM = em
	return nil
}

func (ac *AccCtrl) refreshConditions() error {
	ac.logger.Info("generating new deposit condition as current one expires in 24h")
	timeoutAt := time.Now().AddDate(0, 0, int(ac.Config.App.Account.AddressValidityTimeoutDays))
	newDepCond, err := ac.Acc.AllocateDepositAddress(&deposit.Conditions{TimeoutAt: &timeoutAt, MultiUse: true})
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(newDepCond); err != nil {
		return err
	}
	if err := ioutil.WriteFile(currentCondsFile, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}
	ac.current = newDepCond
	return nil
}

// GenerateNewDonationAddress returns the current valid deposit conditions or a new one
func (ac *AccCtrl) GenerateNewDonationAddress() (*deposit.CDA, error) {
	ac.checkCondMu.Lock()
	defer ac.checkCondMu.Unlock()
	// if the current deposit address will expire within 24 hours, we generate a new one
	if ac.current == nil || ac.current.TimeoutAt.Before(time.Now().AddDate(0, 0, 1)) {
		if err := ac.refreshConditions(); err != nil {
			return nil, err
		}
	}

	return ac.current, nil
}
