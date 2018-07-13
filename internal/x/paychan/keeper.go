package paychan

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/wire"
	"github.com/cosmos/cosmos-sdk/x/bank"
)

// keeper of the paychan store
// Handles validation internally. Does not rely on calling code to do validation.
// Aim to keep public methids safe, private ones not necessaily.
type Keeper struct {
	storeKey   sdk.StoreKey
	cdc        *wire.Codec // needed to serialize objects before putting them in the store
	coinKeeper bank.Keeper

	// codespace
	//codespace sdk.CodespaceType // ??
}

// Called when creating new app.
//func NewKeeper(cdc *wire.Codec, key sdk.StoreKey, ck bank.Keeper, codespace sdk.CodespaceType) Keeper {
func NewKeeper(cdc *wire.Codec, key sdk.StoreKey, ck bank.Keeper) Keeper {
	keeper := Keeper{
		storeKey:   key,
		cdc:        cdc,
		coinKeeper: ck,
		//codespace:  codespace,
	}
	return keeper
}

// bunch of business logic ...

// Reteive a payment channel struct from the blockchain store.
// They are indexed by a concatenation of sender address, receiver address, and an integer.
func (k Keeper) GetPaychan(ctx sdk.Context, sender sdk.Address, receiver sdk.Address, id int64) (Paychan, bool) {
	// Return error as second argument instead of bool?
	var pych Paychan
	// load from DB
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(paychanKey(sender, receiver, id))
	if bz == nil {
		return pych, false
	}
	// unmarshal
	k.cdc.MustUnmarshalBinary(bz, &pych)
	// return
	return pych, true
}

// Store payment channel struct in blockchain store.
func (k Keeper) setPaychan(ctx sdk.Context, pych Paychan) {
	store := ctx.KVStore(k.storeKey)
	// marshal
	bz := k.cdc.MustMarshalBinary(pych) // panics if something goes wrong
	// write to db
	pychKey := paychanKey(pych.sender, pych.receiver, pych.id)
	store.Set(pychKey, bz) // panics if something goes wrong
}

// Create a new payment channel and lock up sender funds.
func (k Keeper) CreatePaychan(ctx sdk.Context, sender sdk.Address, receiver sdk.Address, amount sdk.Coins) (sdk.Tags, sdk.Error) {
	// TODO move validation somewhere nicer
	// args present
	if len(sender) == 0 {
		return nil, sdk.ErrInvalidAddress(sender.String())
	}
	if len(receiver) == 0 {
		return nil, sdk.ErrInvalidAddress(receiver.String())
	}
	if len(amount) == 0 {
		return nil, sdk.ErrInvalidCoins(amount.String())
	}
	// Check if coins are sorted, non zero, positive
	if !amount.IsValid() {
		return nil, sdk.ErrInvalidCoins(amount.String())
	}
	if !amount.IsPositive() {
		return nil, sdk.ErrInvalidCoins(amount.String())
	}
	// sender should exist already as they had to sign.
	// receiver address exists. am is the account mapper in the coin keeper.
	// TODO automatically create account if not present?
	// TODO remove as account mapper not available to this pkg
	//if k.coinKeeper.am.GetAccount(ctx, receiver) == nil {
	//	return nil, sdk.ErrUnknownAddress(receiver.String())
	//}

	// sender has enough coins - done in Subtract method
	// TODO check if sender and receiver different?

	// Calculate next id (num existing paychans plus 1)
	id := int64(len(k.GetPaychans(sender, receiver)) + 1) // TODO check for overflow?
	// subtract coins from sender
	_, tags, err := k.coinKeeper.SubtractCoins(ctx, sender, amount)
	if err != nil {
		return nil, err
	}
	// create new Paychan struct
	pych := Paychan{
		sender:   sender,
		receiver: receiver,
		id:       id,
		balance:  amount,
	}
	// save to db
	k.setPaychan(ctx, pych)

	// TODO create tags
	//tags := sdk.NewTags()
	return tags, err
}

// Close a payment channel and distribute funds to participants.
func (k Keeper) ClosePaychan(ctx sdk.Context, sender sdk.Address, receiver sdk.Address, id int64, receiverAmount sdk.Coins) (sdk.Tags, sdk.Error) {
	if len(sender) == 0 {
		return nil, sdk.ErrInvalidAddress(sender.String())
	}
	if len(receiver) == 0 {
		return nil, sdk.ErrInvalidAddress(receiver.String())
	}
	if len(receiverAmount) == 0 {
		return nil, sdk.ErrInvalidCoins(receiverAmount.String())
	}
	// check id ≥ 0
	if id < 0 {
		return nil, sdk.ErrInvalidAddress(strconv.Itoa(int(id))) // TODO implement custom errors
	}

	// Check if coins are sorted, non zero, non negative
	if !receiverAmount.IsValid() {
		return nil, sdk.ErrInvalidCoins(receiverAmount.String())
	}
	if !receiverAmount.IsPositive() {
		return nil, sdk.ErrInvalidCoins(receiverAmount.String())
	}

	store := ctx.KVStore(k.storeKey)

	pych, exists := k.GetPaychan(ctx, sender, receiver, id)
	if !exists {
		return nil, sdk.ErrUnknownAddress("paychan not found") // TODO implement custom errors
	}
	// compute coin distribution
	senderAmount := pych.balance.Minus(receiverAmount) // Minus sdk.Coins method
	// check that receiverAmt not greater than paychan balance
	if !senderAmount.IsNotNegative() {
		return nil, sdk.ErrInsufficientFunds(pych.balance.String())
	}
	// add coins to sender
	// creating account if it doesn't exist
	k.coinKeeper.AddCoins(ctx, sender, senderAmount)
	// add coins to receiver
	k.coinKeeper.AddCoins(ctx, receiver, receiverAmount)

	// delete paychan from db
	pychKey := paychanKey(pych.sender, pych.receiver, pych.id)
	store.Delete(pychKey)

	// TODO create tags
	//sdk.NewTags(
	//	"action", []byte("channel closure"),
	//	"receiver", receiver.Bytes(),
	//	"sender", sender.Bytes(),
	//	"id", ??)
	tags := sdk.NewTags()
	return tags, nil
}

// Creates a key to reference a paychan in the blockchain store.
func paychanKey(sender sdk.Address, receiver sdk.Address, id int64) []byte {

	//sdk.Address is just a slice of bytes under a different name
	//convert id to string then to byte slice
	idAsBytes := []byte(strconv.Itoa(int(id)))
	// concat sender and receiver and integer ID
	key := append(sender.Bytes(), receiver.Bytes()...)
	key = append(key, idAsBytes...)
	return key
}

// Get all paychans between a given sender and receiver.
func (k Keeper) GetPaychans(sender sdk.Address, receiver sdk.Address) []Paychan {
	var paychans []Paychan
	// TODO Implement this
	return paychans
}

// maybe getAllPaychans(sender sdk.address) []Paychan
