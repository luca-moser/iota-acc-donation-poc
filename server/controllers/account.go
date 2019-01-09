package controllers

import (
	"bytes"
	"encoding/gob"
	"github.com/beevik/ntp"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/store"
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
	Acc         *account.Account
	iota        *api.API
	store       *store.BadgerStore
	Config      *config.Configuration `inject:""`
	current     *deposit.Conditions
	checkCondMu sync.Mutex
	logger      log15.Logger
}

type ntpclock struct {
	server string
}

func (clock *ntpclock) Now() (time.Time, error) {
	t, err := ntp.Time(clock.server)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "NTP clock error")
	}
	return t.UTC(), nil
}

func (ac *AccCtrl) Init() error {
	logger, _ := utilities.GetLogger("acc")
	ac.logger = logger

	// register deposit condition gob
	gob.Register(deposit.Conditions{})

	// read in existing deposit condition
	if _, err := os.Stat(currentCondsFile); err == nil {
		logger.Info("reading in existing deposit condition")
		currentBytes, err := ioutil.ReadFile(currentCondsFile)
		if err != nil {
			return err
		}
		dec := gob.NewDecoder(bytes.NewReader(currentBytes))
		currentCond := &deposit.Conditions{}
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
	os.MkdirAll(conf.DataDir, os.ModePerm)

	// init store for the account
	badger, err := store.NewBadgerStore(conf.DataDir)
	if err != nil {
		return errors.Wrap(err, "unable to initialize badger store")
	}

	// init NTP time source
	ntpClock := &ntpclock{conf.Time.NTPServer}

	// init account
	acc, err := account.NewAccount(conf.Seed, badger, ac.iota, &account.Settings{
		MWM: conf.MWM, Depth: conf.GTTADepth, SecurityLevel: consts.SecurityLevel(conf.SecurityLevel),
		PromoteReattachInterval: conf.PromoteReattachInterval, TransferPollInterval: conf.TransferPollInterval,
		Clock: ntpClock,
	})
	if err != nil {
		return errors.Wrap(err, "unable to instantiate account")
	}
	ac.Acc = acc
	return nil
}

func (ac *AccCtrl) refreshConditions() error {
	ac.logger.Info("generating new deposit condition as current one expires in 24h")
	timeoutOn := time.Now().AddDate(0, 0, int(ac.Config.App.Account.AddressValidityTimeoutDays))
	newDepCond, err := ac.Acc.NewDepositRequest(&deposit.Request{TimeoutOn: &timeoutOn, MultiUse: true})
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
func (ac *AccCtrl) GenerateNewDonationAddress() (*deposit.Conditions, error) {
	ac.checkCondMu.Lock()
	defer ac.checkCondMu.Unlock()
	// if the current deposit address will be expired within 24 hours, we generate a new one
	if ac.current == nil || ac.current.TimeoutOn.Before(time.Now().AddDate(0, 0, 1)) {
		if err := ac.refreshConditions(); err != nil {
			return nil, err
		}
	}

	return ac.current, nil
}
