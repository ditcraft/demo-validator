package ethereum

import (
	"context"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ditcraft/demo-validator/database"
	"github.com/ditcraft/demo-validator/smartcontracts/KNWToken"
	"github.com/ditcraft/demo-validator/smartcontracts/KNWVoting"
	"github.com/ditcraft/demo-validator/smartcontracts/ditDemoCoordinator"
	"github.com/ditcraft/demo-validator/smartcontracts/ditToken"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/golang/glog"
)

var lastBlock = uint64(0)
var mutex = &sync.Mutex{}

// WatchEvents will start the eventwatcher
func WatchEvents() {
	connection, err := getConnection()
	if err != nil {
		glog.Error(err)
	}

	ditCooordinatorInstance, err := getDitDemoCoordinatorInstance(connection)
	if err != nil {
		glog.Error(err)
	}

	for {
		events, err := ditCooordinatorInstance.FilterProposeCommit(&bind.FilterOpts{Context: context.Background(), Start: lastBlock}, nil, nil, nil)
		if err != nil {
			glog.Error(err)
			time.Sleep(2 * time.Second)
			connection, err = getConnection()
			if err != nil {
				glog.Error(err)
			}
			time.Sleep(2 * time.Second)
		} else {
			var currentBlock uint64
			for events.Next() {
				currentBlock = events.Event.Raw.BlockNumber
				if lastBlock > 0 {
					header, err := connection.HeaderByNumber(context.Background(), big.NewInt(int64(events.Event.Raw.BlockNumber)))
					if err != nil && err.Error() != "missing required field 'mixHash' for Header" {
						glog.Error(err)
					}
					blockTime := header.Time
					go handleNewProposal(events.Event.Repository, events.Event.Who.Hex(), events.Event.Proposal, blockTime)
				}
			}

			if currentBlock >= lastBlock {
				lastBlock = currentBlock + 1
			}

			time.Sleep(5 * time.Second)
		}
	}
}

func handleNewProposal(_repoHash [32]byte, _address string, _proposalID *big.Int, _currentTime uint64) {
	user, err := database.GetUser(_address)
	username := "unknown"
	if err != nil {
		glog.Error(err)
	} else if user != nil {
		username = user.TwitterScreenName
		if !user.HasUsedClient {
			user.HasUsedClient = true
			err = database.UpdateUser(*user)
			if err != nil {
				glog.Error(err)
			}
		}
	}

	glog.Infof("[PID%d/'%s'] New Proposal received", _proposalID.Int64(), username)

	mutex.Lock()
	glog.Infof("[PID%d/'%s'] Voting on proposal", _proposalID.Int64(), username)
	KNWVoteID, choiceArray, commitEnd, revealEnd, KNWVotingInstance, err := vote(_repoHash, _proposalID)
	mutex.Unlock()
	if err != nil {
		glog.Error(err)
		return
	}
	glog.Infof("[KNW%d/'%s'] Voted on proposal", KNWVoteID.Int64(), username)

	waitTimeCommit := new(big.Int).Sub(commitEnd, big.NewInt(int64(_currentTime)))
	waitTimeReveal := new(big.Int).Sub(revealEnd, commitEnd)

	commitPeriodActive, err := KNWVotingInstance.CommitPeriodActive(nil, KNWVoteID)
	if err != nil {
		glog.Error(err)
		return
	}

	glog.Infof("[KNW%d/'%s'] Waiting for commit phase to end", KNWVoteID.Int64(), username)
	time.Sleep(time.Duration(waitTimeCommit.Int64()) * time.Second)

	newCommitPeriodActive := commitPeriodActive
	for newCommitPeriodActive == commitPeriodActive {
		newCommitPeriodActive, err = KNWVotingInstance.CommitPeriodActive(nil, KNWVoteID)
		if err != nil {
			glog.Error(err)
			return
		}
		time.Sleep(3 * time.Second)
	}

	mutex.Lock()
	glog.Infof("[KNW%d/'%s'] Opening votes", KNWVoteID.Int64(), username)
	err = open(_repoHash, _proposalID, KNWVoteID, choiceArray)
	mutex.Unlock()
	if err != nil {
		glog.Error(err)
		return
	}
	glog.Infof("[KNW%d/'%s'] Opened votes", KNWVoteID.Int64(), username)

	revealPeriodActive, err := KNWVotingInstance.OpenPeriodActive(nil, KNWVoteID)
	if err != nil {
		glog.Error(err)
		return
	}

	glog.Infof("[KNW%d/'%s'] Waiting for opening phase to end", KNWVoteID.Int64(), username)
	time.Sleep(time.Duration(waitTimeReveal.Int64()) * time.Second)

	newRevealPeriodActive := revealPeriodActive
	for newRevealPeriodActive == revealPeriodActive {
		newRevealPeriodActive, err = KNWVotingInstance.OpenPeriodActive(nil, KNWVoteID)
		if err != nil {
			glog.Error(err)
			return
		}
		time.Sleep(3 * time.Second)
	}

	mutex.Lock()
	glog.Infof("[KNW%d/'%s'] Finalizing votes", KNWVoteID.Int64(), username)
	err = finalize(_repoHash, _proposalID, KNWVoteID)
	mutex.Unlock()
	if err != nil {
		glog.Error(err)
		return
	}
	glog.Infof("[KNW%d/'%s'] Finalized votes", KNWVoteID.Int64(), username)
}

// Approve will set the token allowance for the accounts to the maximum
func Approve() error {
	connection, err := getConnection()
	if err != nil {
		return err
	}

	ditTokenInstance, err := getDitTokenInstance(connection)
	if err != nil {
		return err
	}

	for i := 0; i < 5; i++ {
		// Crerating the transaction (basic values)
		auth, err := populateTx(connection, os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i)), os.Getenv("ETHEREUM_PK_"+strconv.Itoa(i)))
		if err != nil {
			return err
		}

		myBalance, err := ditTokenInstance.BalanceOf(nil, common.HexToAddress(os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i))))
		if err != nil {
			return err
		}

		_, err = ditTokenInstance.Approve(auth, common.HexToAddress(os.Getenv("CONTRACT_DIT_COORDINATOR")), myBalance)
		if err != nil {
			return err
		}
	}

	return nil
}

func vote(_repoHash [32]byte, _proposalID *big.Int) (*big.Int, []*big.Int, *big.Int, *big.Int, *KNWVoting.KNWVoting, error) {
	connection, err := getConnection()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	lastAddress := common.HexToAddress(os.Getenv("ETHEREUM_ADDRESS_4"))

	// Create a new instance of the ditContract to access it
	ditCooordinatorInstance, err := getDitDemoCoordinatorInstance(connection)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Create a new instance of the KNWVoting contract to access it
	KNWVotingInstance, err := getKNWVotingInstance(connection)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Create a new instance of the KNWVoting contract to access it
	KNWTokenInstance, err := getKNWTokenInstance(connection)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Retrieving the proposal object from the ditContract
	proposal, err := ditCooordinatorInstance.ProposalsOfRepository(nil, _repoHash, _proposalID)
	if err != nil {
		return nil, nil, nil, nil, nil, errors.New("Failed to retrieve proposal")
	}

	pollMap, err := KNWVotingInstance.Votes(nil, proposal.KNWVoteID)
	if err != nil {
		return nil, nil, nil, nil, nil, errors.New("Failed to retrieve pollMap")
	}

	// Verifiying whether the proposal is valid (if KNWVoteID is zero its not valid or non existent)
	if proposal.KNWVoteID.Int64() == 0 {
		return nil, nil, nil, nil, nil, errors.New("Invalid proposalID")
	}

	// Verifying whether the commit period of this vote is active
	commitPeriodActive, err := KNWVotingInstance.CommitPeriodActive(nil, proposal.KNWVoteID)
	if err != nil {
		return nil, nil, nil, nil, nil, errors.New("Failed to retrieve opening status")
	}

	// If it is now active it's probably over
	if !commitPeriodActive {
		return nil, nil, nil, nil, nil, errors.New("The commit phase of this vote has ended")
	}

	// Verifying whether the commit period of this vote is active
	oldDidCommit, err := KNWVotingInstance.DidCommit(nil, lastAddress, proposal.KNWVoteID)
	if err != nil {
		return nil, nil, nil, nil, nil, errors.New("Failed to retrieve opening status")
	}

	var choices []*big.Int
	for i := 0; i < 5; i++ {
		// In order to create a valid abi-encoded hash of the vote choice and salt
		// we need to create an abi object
		uint256Type, _ := abi.NewType("uint256", nil)
		arguments := abi.Arguments{
			{
				Type: uint256Type,
			},
			{
				Type: uint256Type,
			},
		}

		// rand.Seed(int64(time.Now().Nanosecond()) + int64(i))
		// choices = append(choices, big.NewInt(int64(rand.Intn(2))))
		choice := int64(i % 2)
		if i == 0 {
			choice = int64(1)
		}
		choices = append(choices, big.NewInt(choice))
		// We will now put pack this abi object into a bytearray
		bytes, _ := arguments.Pack(
			choices[i],
			choices[i],
		)

		// And finally hash this bytearray with keccak256, resulting in the votehash
		voteHash := crypto.Keccak256Hash(bytes)

		// Crerating the transaction (basic values)
		auth, err := populateTx(connection, os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i)), os.Getenv("ETHEREUM_PK_"+strconv.Itoa(i)))
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}

		KNWBalance, err := KNWTokenInstance.FreeBalanceOfID(nil, common.HexToAddress(os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i))), proposal.KnowledgeID)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}

		// Voting on the proposal
		_, err = ditCooordinatorInstance.VoteOnProposal(auth, _repoHash, _proposalID, voteHash, KNWBalance)
		if err != nil {
			if strings.Contains(err.Error(), "insufficient funds") {
				return nil, nil, nil, nil, nil, errors.New("Your account doesn't have enough xDai to pay for the transaction")
			}
			return nil, nil, nil, nil, nil, errors.New("Failed to commit the vote: " + err.Error())
		}
	}

	// Waiting for the voting transaction to be mined
	waitingFor := 0
	newDidCommit := oldDidCommit
	for newDidCommit == oldDidCommit {
		waitingFor += 3
		time.Sleep(3 * time.Second)
		// Checking the commit status of the user every 5 seconds
		newDidCommit, err = KNWVotingInstance.DidCommit(nil, lastAddress, proposal.KNWVoteID)
		if err != nil {
			return nil, nil, nil, nil, nil, errors.New("Failed to retrieve commit status")
		}
		// If we are waiting for more than 2 minutes, the transaction might have failed
		if waitingFor > 180 {
			return nil, nil, nil, nil, nil, errors.New("Transaction likely failed")
		}
	}

	return proposal.KNWVoteID, choices, pollMap.CommitEndDate, pollMap.OpenEndDate, KNWVotingInstance, nil
}

func open(_repoHash [32]byte, _proposalID *big.Int, _knwVoteID *big.Int, _choices []*big.Int) error {
	connection, err := getConnection()
	if err != nil {
		return err
	}

	lastAddress := common.HexToAddress(os.Getenv("ETHEREUM_ADDRESS_4"))

	// Create a new instance of the ditContract to access it
	ditCooordinatorInstance, err := getDitDemoCoordinatorInstance(connection)
	if err != nil {
		return err
	}

	// Create a new instance of the KNWVoting contract to access it
	KNWVotingInstance, err := getKNWVotingInstance(connection)
	if err != nil {
		return err
	}

	// Verifying whether the commit period of this vote is active
	revealPeriodActive, err := KNWVotingInstance.OpenPeriodActive(nil, _knwVoteID)
	if err != nil {
		return errors.New("Failed to retrieve opening status")
	}

	// If it is now active it's probably over
	if !revealPeriodActive {
		return errors.New("The reveal phase of this vote has ended")
	}

	// Verifying whether the commit period of this vote is active
	oldDidReveal, err := KNWVotingInstance.DidOpen(nil, lastAddress, _knwVoteID)
	if err != nil {
		return errors.New("Failed to retrieve opening status")
	}

	for i := 0; i < 5; i++ {
		// Crerating the transaction (basic values)
		auth, err := populateTx(connection, os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i)), os.Getenv("ETHEREUM_PK_"+strconv.Itoa(i)))
		if err != nil {
			return err
		}

		// Revealing the vote on the proposal
		_, err = ditCooordinatorInstance.OpenVoteOnProposal(auth, _repoHash, _proposalID, _choices[i], _choices[i])
		if err != nil {
			if strings.Contains(err.Error(), "insufficient funds") {
				return errors.New("Your account doesn't have enough xDai to pay for the transaction")
			}
			return errors.New("Failed to open the vote: " + err.Error())
		}
	}

	// Waiting for the voting transaction to be mined
	waitingFor := 0
	newDidReveal := oldDidReveal
	for newDidReveal == oldDidReveal {
		waitingFor += 3
		time.Sleep(3 * time.Second)
		// Checking the commit status of the user every 5 seconds
		newDidReveal, err = KNWVotingInstance.DidOpen(nil, lastAddress, _knwVoteID)
		if err != nil {
			return errors.New("Failed to retrieve commit status")
		}
		// If we are waiting for more than 2 minutes, the transaction might have failed
		if waitingFor > 180 {
			return errors.New("Transaction likely failed")
		}
	}

	return nil
}

func finalize(_repoHash [32]byte, _proposalID *big.Int, _knwVoteID *big.Int) error {
	connection, err := getConnection()
	if err != nil {
		return err
	}

	lastAddress := common.HexToAddress(os.Getenv("ETHEREUM_ADDRESS_4"))

	// Create a new instance of the ditContract to access it
	ditCooordinatorInstance, err := getDitDemoCoordinatorInstance(connection)
	if err != nil {
		return err
	}

	// Create a new instance of the KNWVoting contract to access it
	KNWVotingInstance, err := getKNWVotingInstance(connection)
	if err != nil {
		return err
	}

	// Verifying whether the commit period of this vote is active
	voteOver, err := KNWVotingInstance.VoteEnded(nil, _knwVoteID)
	if err != nil {
		return errors.New("Failed to retrieve opening status")
	}

	// If it is now active it's probably over
	if !voteOver {
		return errors.New("The reveal phase of this vote hasn't ended yet")
	}

	// Saving the old xDai balance
	oldxDaiBalance, err := connection.BalanceAt(context.Background(), lastAddress, nil)
	if err != nil {
		return errors.New("Failed to retrieve xdai balance")
	}

	for i := 0; i < 5; i++ {
		// Crerating the transaction (basic values)
		auth, err := populateTx(connection, os.Getenv("ETHEREUM_ADDRESS_"+strconv.Itoa(i)), os.Getenv("ETHEREUM_PK_"+strconv.Itoa(i)))
		if err != nil {
			return err
		}

		// Revealing the vote on the proposal
		_, err = ditCooordinatorInstance.FinalizeVote(auth, _repoHash, _proposalID)
		if err != nil {
			if strings.Contains(err.Error(), "insufficient funds") {
				return errors.New("Your account doesn't have enough xDai to pay for the transaction")
			}
			return errors.New("Failed to open the vote: " + err.Error())
		}
	}

	// Waiting for the voting transaction to be mined
	waitingFor := 0
	newxDaiBalance := oldxDaiBalance
	for newxDaiBalance == oldxDaiBalance {
		waitingFor += 3
		time.Sleep(3 * time.Second)
		// Checking the commit status of the user every 5 seconds
		newxDaiBalance, err = connection.BalanceAt(context.Background(), lastAddress, nil)
		if err != nil {
			return errors.New("Failed to retrieve xdai balance status")
		}
		// If we are waiting for more than 2 minutes, the transaction might have failed
		if waitingFor > 180 {
			return errors.New("Transaction likely failed")
		}
	}

	return nil
}

// populateTX will set the necessary values for a ethereum transaction
// amount of gas, gasprice, nonce, sign this with the private key
func populateTx(_connection *ethclient.Client, _address string, _privateKey string) (*bind.TransactOpts, error) {
	// Converting the private key string into a private key object
	privateKey, err := crypto.HexToECDSA(_privateKey)
	if err != nil {
		return nil, errors.New("Failed to convert ethereum private-key")
	}

	// Retrieving the current pending nonce of our address
	pendingNonce, err := _connection.PendingNonceAt(context.Background(), common.HexToAddress(_address))
	if err != nil {
		return nil, errors.New("Failed to retrieve nonce for ethereum transaction")
	}
	// Retrieving the current non-pending nonce of our address
	nonpendingNonce, err := _connection.NonceAt(context.Background(), common.HexToAddress(_address), nil)
	if err != nil {
		return nil, errors.New("Failed to retrieve nonce for ethereum transaction")
	}

	// Edge-Case for slow nodes
	nonce := pendingNonce
	if nonpendingNonce > pendingNonce {
		nonce = nonpendingNonce
	}

	// Retrieving the suggested gasprice by the network
	gasPrice, err := _connection.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, errors.New("Failed to retrieve the gas-price for ethereum transaction")
	}

	// Minimum gas price is 10 gwei for now, which works best for rinkeby
	// Will be changed later on
	defaultGasPrice := big.NewInt(1000000000)
	if gasPrice.Cmp(defaultGasPrice) != 1 {
		gasPrice = defaultGasPrice
	}
	// Setting the values into the transaction-options object
	auth := bind.NewKeyedTransactor(privateKey)
	auth.Nonce = big.NewInt(int64(nonce))

	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(6000000)
	auth.GasPrice = gasPrice

	return auth, nil
}

func getDitDemoCoordinatorInstance(_connection *ethclient.Client) (*ditDemoCoordinator.DitDemoCoordinator, error) {
	// Convertig the hex-string-formatted address into an address object
	ditDemoCoordinatorAddressObject := common.HexToAddress(os.Getenv("CONTRACT_DIT_COORDINATOR"))

	// Create a new instance of the ditDemoCoordinator to access it
	ditDemoCoordinatorInstance, err := ditDemoCoordinator.NewDitDemoCoordinator(ditDemoCoordinatorAddressObject, _connection)
	if err != nil {
		return nil, errors.New("Failed to find ditDemoCoordinator at provided address")
	}

	return ditDemoCoordinatorInstance, nil
}

func getDitTokenInstance(_connection *ethclient.Client) (*ditToken.MintableERC20, error) {
	// Convertig the hex-string-formatted address into an address object
	ditTokenObject := common.HexToAddress(os.Getenv("CONTRACT_DIT_TOKEN"))

	// Create a new instance of the ditDemoCoordinator to access it
	ditTokenInstance, err := ditToken.NewMintableERC20(ditTokenObject, _connection)
	if err != nil {
		return nil, errors.New("Failed to find ditTokenContract at provided address")
	}

	return ditTokenInstance, nil
}

func getKNWVotingInstance(_connection *ethclient.Client) (*KNWVoting.KNWVoting, error) {
	knwVotingAddressObject := common.HexToAddress(os.Getenv("CONTRACT_KNW_VOTING"))

	// Create a new instance of the KNWVoting contract to access it
	KNWVotingInstance, err := KNWVoting.NewKNWVoting(knwVotingAddressObject, _connection)
	if err != nil {
		return nil, errors.New("Failed to find KNWVoting at provided address")
	}

	return KNWVotingInstance, nil
}

func getKNWTokenInstance(_connection *ethclient.Client) (*KNWToken.KNWToken, error) {
	KNWTokenAddress := common.HexToAddress(os.Getenv("CONTRACT_KNW_TOKEN"))

	// Create a new instance of the KNWToken contract to access it
	KNWTokenInstance, err := KNWToken.NewKNWToken(KNWTokenAddress, _connection)
	if err != nil {
		return nil, errors.New("Failed to find KNWToken at provided address")
	}

	return KNWTokenInstance, nil
}

// getConnection will return a connection to the ethereum blockchain
func getConnection() (*ethclient.Client, error) {
	connection, err := ethclient.Dial(os.Getenv("ETHEREUM_RPC"))
	if err != nil {
		return nil, err
	}

	return connection, nil
}
