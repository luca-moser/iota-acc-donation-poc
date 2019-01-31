package main

import (
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/event"
	"github.com/iotaledger/iota.go/account/event/listener"
)

func NewLogPlugin(em event.EventMachine) account.Plugin {
	return &logplugin{em: em, exit: make(chan struct{})}
}

type logplugin struct {
	em   event.EventMachine
	acc  account.Account
	exit chan struct{}
}

func (l *logplugin) Name() string {
	return "logger"
}

func (l *logplugin) Start(acc account.Account) error {
	l.acc = acc
	l.log()
	return nil
}

func (l *logplugin) Shutdown() error {
	l.exit <- struct{}{}
	return nil
}

func (l *logplugin) log() {
	lis := listener.NewEventListener(l.em).
		Promotions().Reattachments().
		ConfirmedSends().Sends().
		ReceivedMessages().ReceivingDeposits().ReceivedDeposits()

	go func() {
		defer lis.Close()
	exit:
		for {
			select {
			case ev := <-lis.Promotion:
				logger.Infof("(event) promoted %s with %s\n", ev.BundleHash[:10], ev.PromotionTailTxHash)
			case ev := <-lis.Reattachment:
				logger.Infof("(event) reattached %s with %s\n", ev.BundleHash[:10], ev.ReattachmentTailTxHash)
			case ev := <-lis.Sending:
				tail := ev[0]
				logger.Infof("(event) sending %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.Sent:
				tail := ev[0]
				logger.Infof("(event) send (confirmed) %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivingDeposit:
				tail := ev[0]
				logger.Infof("(event) receiving deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivedDeposit:
				tail := ev[0]
				logger.Infof("(event) received deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
				printBalance(l.acc)
			case ev := <-lis.ReceivedMessage:
				tail := ev[0]
				logger.Infof("(event) received msg %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case err := <-lis.InternalError:
				logger.Errorf("received internal error: %s\n", err.Error())
			case <-l.exit:
				break exit
			}
		}
	}()
}
