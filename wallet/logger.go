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
	lis := listener.NewChannelEventListener(l.em).
		RegPromotions().
		RegReattachments().
		RegConfirmedTransfers().
		RegSentTransfers().
		RegReceivedMessages().
		RegReceivingDeposits().
		RegReceivedDeposits().
		RegInputSelection().
		RegPreparingTransfer().
		RegGettingTransactionsToApprove().
		RegAttachingToTangle()

	go func() {
		defer lis.Close()
	exit:
		for {
			select {
			case ev := <-lis.Promoted:
				logger.Infof("(event) promoted %s with %s\n", ev.BundleHash[:10], ev.PromotionTailTxHash)
			case ev := <-lis.Reattached:
				logger.Infof("(event) reattached %s with %s\n", ev.BundleHash[:10], ev.ReattachmentTailTxHash)
			case ev := <-lis.SentTransfer:
				tail := ev[0]
				logger.Infof("(event) sent %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.TransferConfirmed:
				tail := ev[0]
				logger.Infof("(event) transfer confirmed %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivingDeposit:
				tail := ev[0]
				logger.Infof("(event) receiving deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivedDeposit:
				tail := ev[0]
				logger.Infof("(event) received deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
				go printBalance(l.acc)
			case ev := <-lis.ReceivedMessage:
				tail := ev[0]
				logger.Infof("(event) received msg %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case balanceCheck := <-lis.ExecutingInputSelection:
				logger.Infof("(event) executing input selection (balance check: %v) \n", balanceCheck)
			case <-lis.PreparingTransfers:
				logger.Infof("(event) preparing transfers\n")
			case <-lis.GettingTransactionsToApprove:
				logger.Infof("(event) getting transactions to approve\n")
			case <-lis.AttachingToTangle:
				logger.Infof("(event) executing proof of work\n")
			case err := <-lis.InternalError:
				logger.Errorf("received internal error: %s\n", err.Error())
			case <-l.exit:
				break exit
			}
		}
	}()
}
