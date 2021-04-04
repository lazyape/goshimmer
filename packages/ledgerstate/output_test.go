package ledgerstate

import (
	"bytes"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/objectstorage"
	"github.com/stretchr/testify/assert"
)

func TestAliasOutput_NewAliasOutputMint(t *testing.T) {
	t.Run("CASE: Happy path", func(t *testing.T) {
		stateAddy := randEd25119Address()
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: 100}, stateAddy)
		assert.NoError(t, err)
		iotaBal, ok := alias.Balances().Get(ColorIOTA)
		assert.True(t, ok)
		assert.Equal(t, uint64(100), iotaBal)
		assert.True(t, alias.GetStateAddress().Equals(stateAddy))
		assert.Nil(t, alias.GetImmutableData())
	})

	t.Run("CASE: Happy path with immutable data", func(t *testing.T) {
		stateAddy := randEd25119Address()
		data := []byte("dummy")
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: 100}, stateAddy, data)
		assert.NoError(t, err)
		iotaBal, ok := alias.Balances().Get(ColorIOTA)
		assert.True(t, ok)
		assert.Equal(t, uint64(100), iotaBal)
		assert.True(t, alias.GetStateAddress().Equals(stateAddy))
		assert.True(t, bytes.Equal(alias.GetImmutableData(), data))
	})

	t.Run("CASE: Below dust threshold", func(t *testing.T) {
		stateAddy := randEd25119Address()
		data := []byte("dummy")
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: DustThresholdAliasOutputIOTA - 1}, stateAddy, data)
		assert.Error(t, err)
		assert.Nil(t, alias)
	})

	t.Run("CASE: State address is an alias", func(t *testing.T) {
		stateAddy := randAliasAddress()
		data := []byte("dummy")
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: 100}, stateAddy, data)
		assert.Error(t, err)
		assert.Nil(t, alias)
	})

	t.Run("CASE: Non existent state address", func(t *testing.T) {
		data := []byte("dummy")
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: 100}, nil, data)
		assert.Error(t, err)
		assert.Nil(t, alias)
	})

	t.Run("CASE: Too big state data", func(t *testing.T) {
		stateAddy := randAliasAddress()
		data := make([]byte, MaxOutputPayloadSize+1)
		alias, err := NewAliasOutputMint(map[Color]uint64{ColorIOTA: 100}, stateAddy, data)
		assert.Error(t, err)
		assert.Nil(t, alias)
	})
}

func TestAliasOutput_NewAliasOutputNext(t *testing.T) {
	originAlias := dummyAliasOutput()

	t.Run("CASE: Happy path, no governance update", func(t *testing.T) {
		nextAlias := originAlias.NewAliasOutputNext()
		assert.True(t, originAlias.GetAliasAddress().Equals(nextAlias.GetAliasAddress()))
		assert.True(t, originAlias.GetStateAddress().Equals(nextAlias.GetStateAddress()))
		assert.True(t, originAlias.GetGoverningAddress().Equals(nextAlias.GetGoverningAddress()))
		// outputid is actually irrelevant here
		assert.True(t, bytes.Equal(nextAlias.ID().Bytes(), originAlias.ID().Bytes()))
	})

	// TODO: to be continued
}

func TestAliasOutput_Clone(t *testing.T) {
	out := dummyAliasOutput()
	outBack := out.Clone()
	outBackT, ok := outBack.(*AliasOutput)
	assert.True(t, ok)
	assert.True(t, out != outBackT)
	assert.True(t, out.stateAddress != outBackT.stateAddress)
	assert.True(t, out.governingAddress != outBackT.governingAddress)
	assert.True(t, notSameMemory(out.immutableData, outBackT.immutableData))
	assert.True(t, notSameMemory(out.stateData, outBackT.stateData))
	assert.EqualValues(t, out.Bytes(), outBack.Bytes())
}

func TestExtendedLockedOutput_Clone(t *testing.T) {
	out := dummyExtendedLockedOutput()
	outBack := out.Clone()
	outBackT, ok := outBack.(*ExtendedLockedOutput)
	assert.True(t, ok)
	assert.True(t, out != outBackT)
	assert.True(t, notSameMemory(out.payload, outBackT.payload))
	assert.True(t, out.address != outBackT.address)
	assert.True(t, out.fallbackAddress != outBackT.fallbackAddress)
	assert.EqualValues(t, out.Bytes(), outBack.Bytes())
}

func notSameMemory(s1, s2 []byte) bool {
	if s1 == nil || s2 == nil {
		return true
	}
	return &s1[cap(s1)-1] != &s2[cap(s2)-1]
}

func dummyAliasOutput() *AliasOutput {
	return &AliasOutput{
		outputID:            randOutputID(),
		outputIDMutex:       sync.RWMutex{},
		balances:            NewColoredBalances(map[Color]uint64{ColorIOTA: 100}),
		aliasAddress:        *randAliasAddress(),
		stateAddress:        randEd25119Address(),
		stateIndex:          0,
		stateData:           []byte("initial"),
		immutableData:       []byte("don't touch this"),
		isGovernanceUpdate:  false,
		governingAddress:    randAliasAddress(),
		StorableObjectFlags: objectstorage.StorableObjectFlags{},
	}
}

func dummyExtendedLockedOutput() *ExtendedLockedOutput {
	return &ExtendedLockedOutput{
		id:                  randOutputID(),
		idMutex:             sync.RWMutex{},
		balances:            NewColoredBalances(map[Color]uint64{ColorIOTA: 1}),
		address:             randEd25119Address(),
		fallbackAddress:     randEd25119Address(),
		fallbackDeadline:    time.Unix(1001, 0),
		timelock:            time.Unix(2000, 0),
		payload:             []byte("a payload"),
		StorableObjectFlags: objectstorage.StorableObjectFlags{},
	}
}

func randEd25119Address() *ED25519Address {
	keyPair := ed25519.GenerateKeyPair()
	return NewED25519Address(keyPair.PublicKey)
}

func randAliasAddress() *AliasAddress {
	randOutputIDBytes := make([]byte, 34)
	_, _ = rand.Read(randOutputIDBytes)
	return NewAliasAddress(randOutputIDBytes)
}

func randOutputID() OutputID {
	randOutputIDBytes := make([]byte, 34)
	_, _ = rand.Read(randOutputIDBytes)
	outputID, _, _ := OutputIDFromBytes(randOutputIDBytes)
	return outputID
}