package controllers

import (
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/store"
	"github.com/iotaledger/iota.go/api"
	"github.com/iotaledger/iota.go/consts"
	"github.com/luca-moser/donapoc/server/server/config"
	"github.com/pkg/errors"
	"net/http"
	"os"
	"time"
)

type AccCtrl struct {
	Acc    *account.Account
	iota   *api.API
	store  *store.BadgerStore
	Config *config.Configuration `inject:""`
}

func (ac *AccCtrl) Init() error {
	conf := ac.Config.App.Account

	// init quorumed (what a word) api
	httpClient := &http.Client{Timeout: time.Duration(5) * time.Second}
	a, err := api.ComposeAPI(api.QuorumHTTPClientSettings{
		PrimaryNode:         &conf.PrimaryNode,
		Threshold:           1, // all nodes must reply with the same response
		NoResponseTolerance: 0,
		Client:              httpClient,
		Nodes:               conf.QuorumNodes,
	}, api.NewQuorumHTTPClient)
	if err != nil {
		return errors.Wrap(err, "unable to construct IOTA API")
	}
	ac.iota = a

	// make sure data dir exists
	os.MkdirAll(conf.DataDir, os.ModePerm)

	// init account
	badger, err := store.NewBadgerStore(conf.DataDir)
	if err != nil {
		return errors.Wrap(err, "unable to initialize badger store")
	}
	acc, err := account.NewAccount(conf.Seed, badger, ac.iota, &account.AccountsOpts{
		MWM: conf.MWM, Depth: conf.GTTADepth, SecurityLevel: consts.SecurityLevel(conf.SecurityLevel),
		PromoteReattachInterval: conf.PromoteReattachInterval, TransferPollInterval: conf.TransferPollInterval,
	})
	if err != nil {
		return errors.Wrap(err, "unable to instantiate account")
	}
	ac.Acc = acc
	return nil
}

func (ac *AccCtrl) GenerateNewDonationAddress() (*deposit.Conditions, error) {
	timeoutOn := time.Now().AddDate(0, 0, int(ac.Config.App.Account.AddressValidityTimeoutDays))
	req := &deposit.Request{TimeoutOn: &timeoutOn}
	return ac.Acc.NewDepositRequest(req)
}
