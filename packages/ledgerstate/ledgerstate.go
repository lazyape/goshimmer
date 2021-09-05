package ledgerstate

import (
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/kvstore/mapdb"

	"github.com/iotaledger/goshimmer/packages/database"
)

// region Ledgerstate //////////////////////////////////////////////////////////////////////////////////////////////////

type Ledgerstate struct {
	Options *Options

	*UTXODAG
	*BranchDAG
	ConfirmationOracle
}

func New(options ...Option) (ledgerstate *Ledgerstate) {
	ledgerstate = &Ledgerstate{}
	ledgerstate.Configure(options...)

	ledgerstate.UTXODAG = NewUTXODAG(ledgerstate)
	ledgerstate.BranchDAG = NewBranchDAG(ledgerstate)
	ledgerstate.ConfirmationOracle = NewSimpleConfirmationOracle(ledgerstate)

	return ledgerstate
}

// Configure modifies the configuration of the Ledgerstate.
func (l *Ledgerstate) Configure(options ...Option) {
	if l.Options == nil {
		l.Options = &Options{
			Store: mapdb.NewMapDB(),
		}
	}

	for _, option := range options {
		option(l.Options)
	}
}

// Shutdown marks the Ledgerstate as stopped, so it will not accept any new Transactions.
func (l *Ledgerstate) Shutdown() {
	l.BranchDAG.Shutdown()
	l.UTXODAG.Shutdown()
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

// Option represents the return type of optional parameters that can be handed into the constructor of the Ledgerstate
// to configure its behavior.
type Option func(*Options)

// Options is a container for all configurable parameters of the Ledgerstate.
type Options struct {
	Store             kvstore.KVStore
	CacheTimeProvider *database.CacheTimeProvider
}

// Store is an Option for the Ledgerstate that allows to specify which storage layer is supposed to be used to persist
// data.
func Store(store kvstore.KVStore) Option {
	return func(options *Options) {
		options.Store = store
	}
}

// CacheTimeProvider is an Option for the Tangle that allows to override hard coded cache time.
func CacheTimeProvider(cacheTimeProvider *database.CacheTimeProvider) Option {
	return func(options *Options) {
		options.CacheTimeProvider = cacheTimeProvider
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////