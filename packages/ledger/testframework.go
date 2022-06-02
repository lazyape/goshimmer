package ledger

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/iotaledger/hive.go/generics/event"
	"github.com/iotaledger/hive.go/generics/lo"
	"github.com/iotaledger/hive.go/generics/model"
	"github.com/iotaledger/hive.go/generics/set"
	"github.com/iotaledger/hive.go/serix"
	"github.com/iotaledger/hive.go/stringify"
	"github.com/iotaledger/hive.go/types"

	"github.com/iotaledger/goshimmer/packages/conflictdag"
	"github.com/iotaledger/goshimmer/packages/consensus/gof"
	"github.com/iotaledger/goshimmer/packages/ledger/utxo"
	"github.com/iotaledger/goshimmer/packages/ledger/vm"
)

// region TestFramework ////////////////////////////////////////////////////////////////////////////////////////////////

// TestFramework provides common testing functionality for the ledger package. As such, it helps to easily build an
// UTXO-DAG by specifying transactions outputs/inputs via aliases.
// It makes use of a simplified MockedVM, with MockedTransaction, MockedOutput and MockedInput.
type TestFramework struct {
	// t contains a reference to the testing instance.
	t *testing.T

	// ledger contains a reference to the Ledger instance that the TestFramework is using.
	ledger *Ledger

	// transactionsByAlias contains a dictionary that maps a human-readable alias to a MockedTransaction.
	transactionsByAlias map[string]*MockedTransaction

	// transactionsByAliasMutex contains a mutex that is used to synchronize parallel access to the transactionsByAlias.
	transactionsByAliasMutex sync.RWMutex

	// outputIDsByAlias contains a dictionary that maps a human-readable alias to an OutputID.
	outputIDsByAlias map[string]utxo.OutputID

	// outputIDsByAliasMutex contains a mutex that is used to synchronize parallel access to the outputIDsByAlias.
	outputIDsByAliasMutex sync.RWMutex
}

// NewTestFramework creates a new instance of the TestFramework with one default output "Genesis" which has to be
// consumed by the first transaction.
func NewTestFramework(t *testing.T, options ...Option) (new *TestFramework) {
	new = &TestFramework{
		t:                   t,
		ledger:              New(options...),
		transactionsByAlias: make(map[string]*MockedTransaction),
		outputIDsByAlias:    make(map[string]utxo.OutputID),
	}

	genesisOutput := NewMockedOutput(utxo.EmptyTransactionID, 0)
	genesisOutputMetadata := NewOutputMetadata(genesisOutput.ID())
	genesisOutputMetadata.SetGradeOfFinality(gof.High)

	genesisOutput.ID().RegisterAlias("Genesis")
	new.outputIDsByAlias["Genesis"] = genesisOutput.ID()

	new.ledger.Storage.outputStorage.Store(genesisOutput).Release()
	new.ledger.Storage.outputMetadataStorage.Store(genesisOutputMetadata).Release()

	return new
}

// Transaction gets the created MockedTransaction by the given alias.
// Panics if it doesn't exist.
func (t *TestFramework) Transaction(txAlias string) (tx *MockedTransaction) {
	t.transactionsByAliasMutex.RLock()
	defer t.transactionsByAliasMutex.RUnlock()

	tx, exists := t.transactionsByAlias[txAlias]
	if !exists {
		panic(fmt.Sprintf("tried to retrieve transaction with unknown alias: %s", txAlias))
	}

	return tx
}

// OutputID gets the created utxo.OutputID by the given alias.
// Panics if it doesn't exist.
func (t *TestFramework) OutputID(alias string) (outputID utxo.OutputID) {
	t.outputIDsByAliasMutex.RLock()
	defer t.outputIDsByAliasMutex.RUnlock()

	outputID, exists := t.outputIDsByAlias[alias]
	if !exists {
		panic(fmt.Sprintf("unknown output alias: %s", alias))
	}

	return outputID
}

// TransactionIDs gets all MockedTransaction given by txAliases.
// Panics if an alias doesn't exist.
func (t *TestFramework) TransactionIDs(txAliases ...string) (txIDs utxo.TransactionIDs) {
	txIDs = utxo.NewTransactionIDs()
	for _, expectedBranchAlias := range txAliases {
		txIDs.Add(t.Transaction(expectedBranchAlias).ID())
	}

	return txIDs
}

// BranchIDs gets all conflictdag.BranchIDs given by txAliases.
// Panics if an alias doesn't exist.
func (t *TestFramework) BranchIDs(txAliases ...string) (branchIDs *set.AdvancedSet[utxo.TransactionID]) {
	branchIDs = set.NewAdvancedSet[utxo.TransactionID]()
	for _, expectedBranchAlias := range txAliases {
		if expectedBranchAlias == "MasterBranch" {
			branchIDs.Add(utxo.TransactionID{})
			continue
		}

		branchIDs.Add(t.Transaction(expectedBranchAlias).ID())
	}

	return branchIDs
}

// CreateTransaction creates a transaction with the given alias and outputCount. Inputs for the transaction are specified
// by their aliases where <txAlias.outputCount>. Panics if an input does not exist.
func (t *TestFramework) CreateTransaction(txAlias string, outputCount uint16, inputAliases ...string) {
	mockedInputs := make([]*MockedInput, 0)
	for _, inputAlias := range inputAliases {
		mockedInputs = append(mockedInputs, NewMockedInput(t.OutputID(inputAlias)))
	}

	t.transactionsByAliasMutex.Lock()
	defer t.transactionsByAliasMutex.Unlock()
	tx := NewMockedTransaction(mockedInputs, outputCount)
	tx.ID().RegisterAlias(txAlias)
	t.transactionsByAlias[txAlias] = tx

	t.outputIDsByAliasMutex.Lock()
	defer t.outputIDsByAliasMutex.Unlock()

	for i := uint16(0); i < outputCount; i++ {
		outputID := t.MockOutputFromTx(tx, i)
		outputAlias := txAlias + "." + strconv.Itoa(int(i))

		outputID.RegisterAlias(outputAlias)
		t.outputIDsByAlias[outputAlias] = outputID
	}
}

// IssueTransaction issues the transaction given by txAlias.
func (t *TestFramework) IssueTransaction(txAlias string) (err error) {
	return t.ledger.StoreAndProcessTransaction(context.Background(), t.Transaction(txAlias))
}

func (t *TestFramework) WaitUntilAllTasksProcessed() (self *TestFramework) {
	// time.Sleep(100 * time.Millisecond)
	event.Loop.WaitUntilAllTasksProcessed()
	return t
}

// MockOutputFromTx creates an utxo.OutputID from a given MockedTransaction and outputIndex.
func (t *TestFramework) MockOutputFromTx(tx *MockedTransaction, outputIndex uint16) (mockedOutputID utxo.OutputID) {
	return utxo.NewOutputID(tx.ID(), outputIndex)
}

// AssertConflictDAG asserts the structure of the branch DAG as specified in expectedParents.
// "branch3": {"branch1","branch2"} asserts that "branch3" should have "branch1" and "branch2" as parents.
// It also verifies the reverse mapping, that there is a child reference (conflictdag.ChildBranch)
// from "branch1"->"branch3" and "branch2"->"branch3".
func (t *TestFramework) AssertConflictDAG(expectedParents map[string][]string) {
	// Parent -> child references.
	childBranches := make(map[utxo.TransactionID]*set.AdvancedSet[utxo.TransactionID])

	for branchAlias, expectedParentAliases := range expectedParents {
		currentBranchID := t.Transaction(branchAlias).ID()
		expectedBranchIDs := t.BranchIDs(expectedParentAliases...)

		// Verify child -> parent references.
		t.ConsumeBranch(currentBranchID, func(branch *conflictdag.Conflict[utxo.TransactionID, utxo.OutputID]) {
			assert.Truef(t.t, expectedBranchIDs.Equal(branch.Parents()), "Conflict(%s): expected parents %s are not equal to actual parents %s", currentBranchID, expectedBranchIDs, branch.Parents())
		})

		for _, parentBranchID := range expectedBranchIDs.Slice() {
			if _, exists := childBranches[parentBranchID]; !exists {
				childBranches[parentBranchID] = set.NewAdvancedSet[utxo.TransactionID]()
			}
			childBranches[parentBranchID].Add(currentBranchID)
		}
	}

	// Verify parent -> child references.
	for parentBranchID, childBranchIDs := range childBranches {
		cachedChildBranches := t.ledger.ConflictDAG.Storage.CachedChildBranches(parentBranchID)
		assert.Equalf(t.t, childBranchIDs.Size(), len(cachedChildBranches), "child branches count does not match for parent branch %s, expected=%s, actual=%s", parentBranchID, childBranchIDs, cachedChildBranches.Unwrap())
		cachedChildBranches.Release()

		for _, childBranchID := range childBranchIDs.Slice() {
			assert.Truef(t.t, t.ledger.ConflictDAG.Storage.CachedChildBranch(parentBranchID, childBranchID).Consume(func(childBranch *conflictdag.ChildBranch[utxo.TransactionID]) {}), "could not load ChildBranch %s,%s", parentBranchID, childBranchID)
		}
	}
}

// AssertConflicts asserts conflict membership from conflictID -> branches but also the reverse mapping branch -> conflictIDs.
// expectedConflictAliases should be specified as
// "output.0": {"branch1", "branch2"}
func (t *TestFramework) AssertConflicts(expectedConflictsAliases map[string][]string) {
	// Conflict -> conflictIDs.
	branchConflicts := make(map[utxo.TransactionID]*set.AdvancedSet[utxo.OutputID])

	for outputAlias, expectedConflictMembersAliases := range expectedConflictsAliases {
		conflictID := t.OutputID(outputAlias)
		expectedConflictMembers := t.BranchIDs(expectedConflictMembersAliases...)

		// Check count of conflict members for this conflictID.
		cachedConflictMembers := t.ledger.ConflictDAG.Storage.CachedConflictMembers(conflictID)
		assert.Equalf(t.t, expectedConflictMembers.Size(), len(cachedConflictMembers), "conflict member count does not match for conflict %s, expected=%s, actual=%s", conflictID, expectedConflictsAliases, cachedConflictMembers.Unwrap())
		cachedConflictMembers.Release()

		// Verify that all named branches are stored as conflict members (conflictID -> branchIDs).
		for _, branchID := range expectedConflictMembers.Slice() {
			assert.Truef(t.t, t.ledger.ConflictDAG.Storage.CachedConflictMember(conflictID, branchID).Consume(func(conflictMember *conflictdag.ConflictMember[utxo.OutputID, utxo.TransactionID]) {}), "could not load ConflictMember %s,%s", conflictID, branchID)

			if _, exists := branchConflicts[branchID]; !exists {
				branchConflicts[branchID] = set.NewAdvancedSet[utxo.OutputID]()
			}
			branchConflicts[branchID].Add(conflictID)
		}
	}

	// Make sure that all branches have all specified conflictIDs (reverse mapping).
	for branchID, expectedConflicts := range branchConflicts {
		t.ConsumeBranch(branchID, func(branch *conflictdag.Conflict[utxo.TransactionID, utxo.OutputID]) {
			assert.Truef(t.t, expectedConflicts.Equal(branch.ConflictIDs()), "%s: conflicts expected=%s, actual=%s", branchID, expectedConflicts, branch.ConflictIDs())
		})
	}
}

// AssertBranchIDs asserts that the given transactions and their outputs are booked into the specified branches.
func (t *TestFramework) AssertBranchIDs(expectedBranches map[string][]string) {
	for txAlias, expectedBranchAliases := range expectedBranches {
		currentTx := t.Transaction(txAlias)

		expectedBranchIDs := t.BranchIDs(expectedBranchAliases...)

		t.ConsumeTransactionMetadata(currentTx.ID(), func(txMetadata *TransactionMetadata) {
			assert.Truef(t.t, expectedBranchIDs.Equal(txMetadata.BranchIDs()), "Transaction(%s): expected %s is not equal to actual %s", txAlias, expectedBranchIDs, txMetadata.BranchIDs())
		})

		t.ConsumeTransactionOutputs(currentTx, func(outputMetadata *OutputMetadata) {
			assert.Truef(t.t, expectedBranchIDs.Equal(outputMetadata.BranchIDs()), "Output(%s): expected %s is not equal to actual %s", outputMetadata.ID(), expectedBranchIDs, outputMetadata.BranchIDs())
		})
	}
}

// AssertBooked asserts the booking status of all given transactions.
func (t *TestFramework) AssertBooked(expectedBookedMap map[string]bool) {
	for txAlias, expectedBooked := range expectedBookedMap {
		currentTx := t.Transaction(txAlias)
		t.ConsumeTransactionMetadata(currentTx.ID(), func(txMetadata *TransactionMetadata) {
			assert.Equalf(t.t, expectedBooked, txMetadata.IsBooked(), "Transaction(%s): expected booked(%s) but has booked(%s)", txAlias, expectedBooked, txMetadata.IsBooked())

			_ = txMetadata.OutputIDs().ForEach(func(outputID utxo.OutputID) (err error) {
				// Check if output exists according to the Booked status of the enclosing Transaction.
				assert.Equalf(t.t, expectedBooked, t.ledger.Storage.CachedOutputMetadata(outputID).Consume(func(_ *OutputMetadata) {}),
					"Output(%s): expected booked(%s) but has booked(%s)", outputID, expectedBooked, txMetadata.IsBooked())
				return nil
			})
		})
	}
}

// AllBooked returns whether all given transactions are booked.
func (t *TestFramework) AllBooked(txAliases ...string) (allBooked bool) {
	for _, txAlias := range txAliases {
		t.ConsumeTransactionMetadata(t.Transaction(txAlias).ID(), func(txMetadata *TransactionMetadata) {
			allBooked = txMetadata.IsBooked()
		})

		if !allBooked {
			return
		}
	}

	return
}

// ConsumeBranch loads and consumes conflictdag.Conflict. Asserts that the loaded entity exists.
func (t *TestFramework) ConsumeBranch(branchID utxo.TransactionID, consumer func(branch *conflictdag.Conflict[utxo.TransactionID, utxo.OutputID])) {
	assert.Truef(t.t, t.ledger.ConflictDAG.Storage.CachedConflict(branchID).Consume(consumer), "failed to load branch %s", branchID)
}

// ConsumeTransactionMetadata loads and consumes TransactionMetadata. Asserts that the loaded entity exists.
func (t *TestFramework) ConsumeTransactionMetadata(txID utxo.TransactionID, consumer func(txMetadata *TransactionMetadata)) {
	assert.Truef(t.t, t.ledger.Storage.CachedTransactionMetadata(txID).Consume(consumer), "failed to load metadata of %s", txID)
}

// ConsumeOutputMetadata loads and consumes OutputMetadata. Asserts that the loaded entity exists.
func (t *TestFramework) ConsumeOutputMetadata(outputID utxo.OutputID, consumer func(outputMetadata *OutputMetadata)) {
	assert.True(t.t, t.ledger.Storage.CachedOutputMetadata(outputID).Consume(consumer))
}

// ConsumeOutput loads and consumes Output. Asserts that the loaded entity exists.
func (t *TestFramework) ConsumeOutput(outputID utxo.OutputID, consumer func(output utxo.Output)) {
	assert.True(t.t, t.ledger.Storage.CachedOutput(outputID).Consume(consumer))
}

// ConsumeTransactionOutputs loads and consumes all OutputMetadata of the given Transaction. Asserts that the loaded entities exists.
func (t *TestFramework) ConsumeTransactionOutputs(mockTx *MockedTransaction, consumer func(outputMetadata *OutputMetadata)) {
	t.ConsumeTransactionMetadata(mockTx.ID(), func(txMetadata *TransactionMetadata) {
		assert.EqualValuesf(t.t, mockTx.M.OutputCount, txMetadata.OutputIDs().Size(), "Output count in %s do not match", mockTx.ID())

		for _, outputID := range txMetadata.OutputIDs().Slice() {
			t.ConsumeOutputMetadata(outputID, consumer)
		}
	})
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region MockedInput //////////////////////////////////////////////////////////////////////////////////////////////////

// MockedInput is a mocked entity that allows to "address" which Outputs are supposed to be used by a Transaction.
type MockedInput struct {
	// outputID contains the referenced OutputID.
	OutputID utxo.OutputID `serix:"0"`
}

// NewMockedInput creates a new MockedInput from an utxo.OutputID.
func NewMockedInput(outputID utxo.OutputID) (new *MockedInput) {
	return &MockedInput{OutputID: outputID}
}

// String returns a human-readable version of the MockedInput.
func (m *MockedInput) String() (humanReadable string) {
	return stringify.Struct("MockedInput",
		stringify.StructField("OutputID", m.OutputID),
	)
}

// utxoInput type-casts the MockedInput to a utxo.Input.
func (m *MockedInput) utxoInput() (input utxo.Input) {
	return m
}

// code contract (make sure the struct implements all required methods).
var _ utxo.Input = new(MockedInput)

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region MockedOutput /////////////////////////////////////////////////////////////////////////////////////////////////

// MockedOutput is the container for the data produced by executing a MockedTransaction.
type MockedOutput struct {
	model.Storable[utxo.OutputID, mockedOutput] `serix:"0"`
}

type mockedOutput struct {
	// TxID contains the identifier of the Transaction that created this MockedOutput.
	TxID utxo.TransactionID `serix:"0"`

	// Index contains the Index of the Output in respect to it's creating Transaction (the nth Output will have the
	// Index n).
	Index uint16 `serix:"1"`
}

// NewMockedOutput creates a new MockedOutput based on the utxo.TransactionID and its index within the MockedTransaction.
func NewMockedOutput(txID utxo.TransactionID, index uint16) (out *MockedOutput) {
	out = &MockedOutput{model.NewStorable[utxo.OutputID](mockedOutput{
		TxID:  txID,
		Index: index,
	})}
	out.SetID(utxo.OutputID{TransactionID: txID, Index: index})
	return out
}

// code contract (make sure the struct implements all required methods).
var _ utxo.Output = new(MockedOutput)

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region MockedTransaction ////////////////////////////////////////////////////////////////////////////////////////////

// MockedTransaction is the type that is used to describe instructions how to modify the ledger state for MockedVM.
type MockedTransaction struct {
	model.Storable[utxo.TransactionID, mockedTransaction] `serix:"0"`
}

type mockedTransaction struct {
	// Inputs contains the list of MockedInput objects that address the consumed Outputs.
	Inputs []*MockedInput `serix:"0,lengthPrefixType=uint16"`

	// OutputCount contains the number of Outputs that this MockedTransaction creates.
	OutputCount uint16 `serix:"1"`

	// UniqueEssence contains a unique value for each created MockedTransaction to ensure a unique TransactionID.
	UniqueEssence uint64 `serix:"2"`
}

// NewMockedTransaction creates a new MockedTransaction with the given inputs and specified outputCount.
// A unique essence is simulated by an atomic counter, incremented globally for each MockedTransaction created.
func NewMockedTransaction(inputs []*MockedInput, outputCount uint16) (tx *MockedTransaction) {
	tx = &MockedTransaction{model.NewStorable[utxo.TransactionID](mockedTransaction{
		Inputs:        inputs,
		OutputCount:   outputCount,
		UniqueEssence: atomic.AddUint64(&_uniqueEssenceCounter, 1),
	})}

	b := types.Identifier{}
	binary.BigEndian.PutUint64(b[:], tx.M.UniqueEssence)
	tx.SetID(utxo.TransactionID{Identifier: b})

	return tx
}

// Inputs returns the inputs of the Transaction.
func (m *MockedTransaction) Inputs() (inputs []utxo.Input) {
	return lo.Map(m.M.Inputs, (*MockedInput).utxoInput)
}

// code contract (make sure the struct implements all required methods).
var _ utxo.Transaction = new(MockedTransaction)

// _uniqueEssenceCounter contains a counter that is used to generate unique TransactionIDs.
var _uniqueEssenceCounter uint64

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region MockedVM /////////////////////////////////////////////////////////////////////////////////////////////////////

// MockedVM is an implementation of UTXO-based VMs for testing purposes.
type MockedVM struct{}

// NewMockedVM creates a new MockedVM.
func NewMockedVM() *MockedVM {
	return new(MockedVM)
}

// ParseTransaction un-serializes a Transaction from the given sequence of bytes.
func (m *MockedVM) ParseTransaction(transactionBytes []byte) (transaction utxo.Transaction, err error) {
	mockedTx := new(MockedTransaction)
	if _, err = serix.DefaultAPI.Decode(context.Background(), transactionBytes, mockedTx, serix.WithValidation()); err != nil {
		return nil, err
	}

	return mockedTx, nil
}

// ParseOutput un-serializes an Output from the given sequence of bytes.
func (m *MockedVM) ParseOutput(outputBytes []byte) (output utxo.Output, err error) {
	newOutput := new(MockedOutput)
	if _, err = serix.DefaultAPI.Decode(context.Background(), outputBytes, newOutput, serix.WithValidation()); err != nil {
		return nil, err
	}

	return newOutput, nil
}

// ResolveInput translates the Input into an OutputID.
func (m *MockedVM) ResolveInput(input utxo.Input) (outputID utxo.OutputID) {
	return input.(*MockedInput).OutputID
}

// ExecuteTransaction executes the Transaction and determines the Outputs from the given Inputs. It returns an error
// if the execution fails.
func (m *MockedVM) ExecuteTransaction(transaction utxo.Transaction, _ *utxo.Outputs, _ ...uint64) (outputs []utxo.Output, err error) {
	mockedTransaction := transaction.(*MockedTransaction)

	outputs = make([]utxo.Output, mockedTransaction.M.OutputCount)
	for i := uint16(0); i < mockedTransaction.M.OutputCount; i++ {
		outputs[i] = NewMockedOutput(mockedTransaction.ID(), i)
		outputs[i].SetID(utxo.NewOutputID(mockedTransaction.ID(), i))
	}

	return
}

// code contract (make sure the struct implements all required methods).
var _ vm.VM = new(MockedVM)

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////