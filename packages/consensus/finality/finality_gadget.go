package finality

import (
	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/datastructure/walker"
	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/types"

	"github.com/iotaledger/goshimmer/packages/consensus/gof"
	"github.com/iotaledger/goshimmer/packages/ledgerstate"
	"github.com/iotaledger/goshimmer/packages/markers"
	"github.com/iotaledger/goshimmer/packages/tangle"
)

type Gadget interface {
	HandleMarker(marker *markers.Marker, aw float64) (err error)
	HandleBranch(branchID ledgerstate.BranchID, aw float64) (err error)
	tangle.ConfirmationOracle
}

// MessageThresholdTranslation is a function which translates approval weight to a gof.GradeOfFinality.
type MessageThresholdTranslation func(aw float64) gof.GradeOfFinality

// BranchThresholdTranslation is a function which translates approval weight to a gof.GradeOfFinality.
type BranchThresholdTranslation func(branchID ledgerstate.BranchID, aw float64) gof.GradeOfFinality

const (
	lowLowerBound    = 0.2
	mediumLowerBound = 0.3
	highLowerBound   = 0.5
)

var (
	// DefaultBranchGoFTranslation is the default function to translate the approval weight to gof.GradeOfFinality of a branch.
	DefaultBranchGoFTranslation BranchThresholdTranslation = func(branchID ledgerstate.BranchID, aw float64) gof.GradeOfFinality {
		switch {
		case aw >= lowLowerBound && aw < mediumLowerBound:
			return gof.Low
		case aw >= mediumLowerBound && aw < highLowerBound:
			return gof.Medium
		case aw >= highLowerBound:
			return gof.High
		default:
			return gof.None
		}
	}

	// DefaultMessageGoFTranslation is the default function to translate the approval weight to gof.GradeOfFinality of a message.
	DefaultMessageGoFTranslation MessageThresholdTranslation = func(aw float64) gof.GradeOfFinality {
		switch {
		case aw >= lowLowerBound && aw < mediumLowerBound:
			return gof.Low
		case aw >= mediumLowerBound && aw < highLowerBound:
			return gof.Medium
		case aw >= highLowerBound:
			return gof.High
		default:
			return gof.None
		}
	}

	// ErrUnsupportedBranchType is returned when an operation is tried on an unsupported branch type.
	ErrUnsupportedBranchType = errors.New("unsupported branch type")
)

// Option is a function setting an option on an Options struct.
type Option func(*Options)

// Options defines the options for a SimpleFinalityGadget.
type Options struct {
	BranchTransFunc        BranchThresholdTranslation
	MessageTransFunc       MessageThresholdTranslation
	BranchGoFReachedLevel  gof.GradeOfFinality
	MessageGoFReachedLevel gof.GradeOfFinality
}

var defaultOpts = []Option{
	WithBranchThresholdTranslation(DefaultBranchGoFTranslation),
	WithMessageThresholdTranslation(DefaultMessageGoFTranslation),
	WithBranchGoFReachedLevel(gof.High),
	WithMessageGoFReachedLevel(gof.High),
}

// WithMessageThresholdTranslation returns an Option setting the MessageThresholdTranslation.
func WithMessageThresholdTranslation(f MessageThresholdTranslation) Option {
	return func(opts *Options) {
		opts.MessageTransFunc = f
	}
}

// WithBranchThresholdTranslation returns an Option setting the BranchThresholdTranslation.
func WithBranchThresholdTranslation(f BranchThresholdTranslation) Option {
	return func(opts *Options) {
		opts.BranchTransFunc = f
	}
}

// WithBranchGoFReachedLevel returns an Option setting the branch reached grade of finality level.
func WithBranchGoFReachedLevel(branchGradeOfFinality gof.GradeOfFinality) Option {
	return func(opts *Options) {
		opts.BranchGoFReachedLevel = branchGradeOfFinality
	}
}

// WithMessageGoFReachedLevel returns an Option setting the message reached grade of finality level.
func WithMessageGoFReachedLevel(msgGradeOfFinality gof.GradeOfFinality) Option {
	return func(opts *Options) {
		opts.MessageGoFReachedLevel = msgGradeOfFinality
	}
}

func SimpleFinalityGadgetFactory(opts ...Option) func(tangle *tangle.Tangle) tangle.ConfirmationOracle {
	return func(tangle *tangle.Tangle) tangle.ConfirmationOracle {
		return NewSimpleFinalityGadget(tangle, opts...)
	}
}

// SimpleFinalityGadget is a Gadget which simply translates approval weight down to gof.GradeOfFinality
// and then applies it to messages, branches, transactions and outputs.
type SimpleFinalityGadget struct {
	tangle *tangle.Tangle
	opts   *Options
	events *tangle.ConfirmationEvents
}

func (s *SimpleFinalityGadget) IsTransactionRejected(transactionID ledgerstate.TransactionID) bool {
	return false
}

func (s *SimpleFinalityGadget) IsBranchRejected(branchID ledgerstate.BranchID) bool {
	return false
}

// NewSimpleFinalityGadget creates a new SimpleFinalityGadget.
func NewSimpleFinalityGadget(t *tangle.Tangle, opts ...Option) *SimpleFinalityGadget {
	sfg := &SimpleFinalityGadget{
		tangle: t,
		opts:   &Options{},
		events: &tangle.ConfirmationEvents{
			MessageConfirmed:     events.NewEvent(tangle.MessageIDCaller),
			TransactionConfirmed: events.NewEvent(ledgerstate.TransactionIDEventHandler),
			BranchConfirmed:      events.NewEvent(ledgerstate.BranchIDEventHandler),
		},
	}

	for _, defOpt := range defaultOpts {
		defOpt(sfg.opts)
	}
	for _, opt := range opts {
		opt(sfg.opts)
	}

	return sfg
}

// Events returns the events this gadget exposes.
func (s *SimpleFinalityGadget) Events() *tangle.ConfirmationEvents {
	return s.events
}

func (s *SimpleFinalityGadget) IsMarkerConfirmed(marker *markers.Marker) (confirmed bool) {
	messageID := s.tangle.Booker.MarkersManager.MessageID(marker)
	if messageID == tangle.EmptyMessageID {
		return false
	}

	s.tangle.Storage.MessageMetadata(messageID).Consume(func(messageMetadata *tangle.MessageMetadata) {
		if messageMetadata.GradeOfFinality() >= s.opts.MessageGoFReachedLevel {
			confirmed = true
		}
	})
	return
}

// IsMessageConfirmed returns whether the given message is confirmed.
func (s *SimpleFinalityGadget) IsMessageConfirmed(msgID tangle.MessageID) (confirmed bool) {
	s.tangle.Storage.MessageMetadata(msgID).Consume(func(messageMetadata *tangle.MessageMetadata) {
		if messageMetadata.GradeOfFinality() >= s.opts.MessageGoFReachedLevel {
			confirmed = true
		}
	})
	return
}

// IsBranchConfirmed returns whether the given branch is confirmed.
func (s *SimpleFinalityGadget) IsBranchConfirmed(branchID ledgerstate.BranchID) (confirmed bool) {
	// TODO: HANDLE ERRORS INSTEAD?
	branchGoF, _ := s.tangle.LedgerState.UTXODAG.BranchGradeOfFinality(branchID)

	return branchGoF >= s.opts.BranchGoFReachedLevel
}

// IsTransactionConfirmed returns whether the given transaction is confirmed.
func (s *SimpleFinalityGadget) IsTransactionConfirmed(transactionID ledgerstate.TransactionID) (confirmed bool) {
	s.tangle.LedgerState.TransactionMetadata(transactionID).Consume(func(transactionMetadata *ledgerstate.TransactionMetadata) {
		if transactionMetadata.GradeOfFinality() >= s.opts.MessageGoFReachedLevel {
			confirmed = true
		}
	})
	return
}

// IsOutputConfirmed returns whether the given output is confirmed.
func (s *SimpleFinalityGadget) IsOutputConfirmed(outputID ledgerstate.OutputID) (confirmed bool) {
	s.tangle.LedgerState.CachedOutputMetadata(outputID).Consume(func(outputMetadata *ledgerstate.OutputMetadata) {
		if outputMetadata.GradeOfFinality() >= s.opts.BranchGoFReachedLevel {
			confirmed = true
		}
	})
	return
}

func (s *SimpleFinalityGadget) HandleMarker(marker *markers.Marker, aw float64) (err error) {
	gradeOfFinality := s.opts.MessageTransFunc(aw)
	if gradeOfFinality == gof.None {
		return
	}

	// get message ID of marker
	messageID := s.tangle.Booker.MarkersManager.MessageID(marker)

	// check that we're updating the GoF
	var gofIncreased bool
	s.tangle.Storage.MessageMetadata(messageID).Consume(func(messageMetadata *tangle.MessageMetadata) {
		if gradeOfFinality > messageMetadata.GradeOfFinality() {
			gofIncreased = true
		}
	})
	if !gofIncreased {
		return
	}

	propagateGoF := func(message *tangle.Message, messageMetadata *tangle.MessageMetadata, w *walker.Walker) {
		// stop walking to past cone if reach a marker with a higher grade of finality
		if messageMetadata.StructureDetails().IsPastMarker && messageMetadata.GradeOfFinality() >= gradeOfFinality {
			return
		}

		// abort if message has GoF already set
		if !s.setMessageGoF(messageMetadata, gradeOfFinality) {
			return
		}

		// TODO: revisit weak parents
		// mark weak parents as finalized but not propagate finalized flag to its past cone
		//message.ForEachParentByType(tangle.WeakParentType, func(parentID tangle.MessageID) {
		//	Tangle().Storage.MessageMetadata(parentID).Consume(func(messageMetadata *tangle.MessageMetadata) {
		//		setMessageGoF(messageMetadata)
		//	})
		//})

		// propagate GoF to strong parents
		message.ForEachParentByType(tangle.StrongParentType, func(parentID tangle.MessageID) {
			w.Push(parentID)
		})
	}

	s.tangle.Utils.WalkMessageAndMetadata(propagateGoF, tangle.MessageIDs{messageID}, false)

	return
}

func (s *SimpleFinalityGadget) HandleBranch(branchID ledgerstate.BranchID, aw float64) (err error) {
	newGradeOfFinality := s.opts.BranchTransFunc(branchID, aw)

	// update GoF of txs within the same branch
	txGoFPropWalker := walker.New()
	txGoFPropWalker.Push(branchID.TransactionID())
	for txGoFPropWalker.HasNext() {
		s.forwardPropagateBranchGoFToTxs(txGoFPropWalker.Next().(ledgerstate.TransactionID), branchID, newGradeOfFinality, txGoFPropWalker)
	}

	if newGradeOfFinality >= s.opts.BranchGoFReachedLevel {
		s.events.BranchConfirmed.Trigger(branchID)
	}

	return err
}

func (s *SimpleFinalityGadget) forwardPropagateBranchGoFToTxs(candidateTxID ledgerstate.TransactionID, candidateBranchID ledgerstate.BranchID, newGradeOfFinality gof.GradeOfFinality, txGoFPropWalker *walker.Walker) bool {
	return s.tangle.LedgerState.UTXODAG.CachedTransactionMetadata(candidateTxID).Consume(func(transactionMetadata *ledgerstate.TransactionMetadata) {
		// we stop if we walk outside our branch
		if transactionMetadata.BranchID() != candidateBranchID {
			return
		}

		var maxAttachmentGoF gof.GradeOfFinality
		s.tangle.Storage.Attachments(transactionMetadata.ID()).Consume(func(attachment *tangle.Attachment) {
			s.tangle.Storage.MessageMetadata(attachment.MessageID()).Consume(func(messageMetadata *tangle.MessageMetadata) {
				if maxAttachmentGoF < messageMetadata.GradeOfFinality() {
					maxAttachmentGoF = messageMetadata.GradeOfFinality()
				}
			})
		})

		// only adjust tx GoF if attachments have at least GoF derived from UTXO parents
		if maxAttachmentGoF < newGradeOfFinality {
			return
		}

		// abort if the grade of finality did not change
		if !transactionMetadata.SetGradeOfFinality(newGradeOfFinality) {
			return
		}

		s.tangle.LedgerState.UTXODAG.CachedTransaction(transactionMetadata.ID()).Consume(func(transaction *ledgerstate.Transaction) {
			// we use a set of consumer txs as our candidate tx can consume multiple outputs from the same txs,
			// but we want to add such tx only once to the walker
			consumerTxs := make(ledgerstate.TransactionIDs)

			// adjust output GoF and add its consumer txs to the walker
			for _, output := range transaction.Essence().Outputs() {
				s.adjustOutputGoF(output, newGradeOfFinality, consumerTxs, txGoFPropWalker)
			}
		})
		if transactionMetadata.GradeOfFinality() >= s.opts.BranchGoFReachedLevel {
			s.events.TransactionConfirmed.Trigger(candidateTxID)
		}
	})
}

func (s *SimpleFinalityGadget) adjustOutputGoF(output ledgerstate.Output, newGradeOfFinality gof.GradeOfFinality, consumerTxs ledgerstate.TransactionIDs, txGoFPropWalker *walker.Walker) bool {
	return s.tangle.LedgerState.UTXODAG.CachedOutputMetadata(output.ID()).Consume(func(outputMetadata *ledgerstate.OutputMetadata) {
		outputMetadata.SetGradeOfFinality(newGradeOfFinality)
		s.tangle.LedgerState.Consumers(output.ID()).Consume(func(consumer *ledgerstate.Consumer) {
			if _, has := consumerTxs[consumer.TransactionID()]; !has {
				consumerTxs[consumer.TransactionID()] = types.Empty{}
				txGoFPropWalker.Push(consumer.TransactionID())
			}
		})
	})
}

func (s *SimpleFinalityGadget) setMessageGoF(messageMetadata *tangle.MessageMetadata, gradeOfFinality gof.GradeOfFinality) (modified bool) {
	// abort if message has GoF already set
	if modified = messageMetadata.SetGradeOfFinality(gradeOfFinality); !modified {
		return
	}

	// set GoF of payload (applicable only to transactions)
	s.setPayloadGoF(messageMetadata.ID(), gradeOfFinality)

	if gradeOfFinality >= s.opts.MessageGoFReachedLevel {
		s.Events().MessageConfirmed.Trigger(messageMetadata.ID())
	}

	return modified
}

func (s *SimpleFinalityGadget) setPayloadGoF(messageID tangle.MessageID, gradeOfFinality gof.GradeOfFinality) {
	s.tangle.Utils.ComputeIfTransaction(messageID, func(transactionID ledgerstate.TransactionID) {
		s.tangle.LedgerState.TransactionMetadata(transactionID).Consume(func(transactionMetadata *ledgerstate.TransactionMetadata) {
			// if the transaction is part of a conflicting branch then we need to evaluate based on branch AW
			if s.tangle.LedgerState.TransactionConflicting(transactionID) {
				branchID := ledgerstate.NewBranchID(transactionID)
				gradeOfFinality = s.opts.BranchTransFunc(branchID, s.tangle.ApprovalWeightManager.WeightOfBranch(branchID))
			}

			// abort if transaction has GoF already set
			if !transactionMetadata.SetGradeOfFinality(gradeOfFinality) {
				return
			}

			// set GoF in outputs
			s.tangle.LedgerState.Transaction(transactionID).Consume(func(transaction *ledgerstate.Transaction) {
				for _, output := range transaction.Essence().Outputs() {
					s.tangle.LedgerState.CachedOutputMetadata(output.ID()).Consume(func(outputMetadata *ledgerstate.OutputMetadata) {
						outputMetadata.SetGradeOfFinality(gradeOfFinality)
					})
				}

				//for _, input := range transaction.Essence().Inputs() {
				//	referencedOutputID := input.(*UTXOInput).ReferencedOutputID()
				//	// TODO: do we still need this?
				//	u.CachedOutputMetadata(referencedOutputID).Consume(func(outputMetadata *OutputMetadata) {
				//		outputMetadata.SetConfirmedConsumer(*transaction.id)
				//	})
				//}
			})

			if gradeOfFinality >= s.opts.BranchGoFReachedLevel {
				s.Events().TransactionConfirmed.Trigger(transactionID)
			}
		})
	})
}

var _ Gadget = &SimpleFinalityGadget{}