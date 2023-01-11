package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Transaction struct {
	Time           time.Time `json:"time"`
	Counterparty   string    `json:"counterparty"`
	Amount         float64   `json:"amount"`
	ClosingBalance float64   `json:"closingBalance"`
	Message        string    `json:"message"`
	Type           string    `json:"type"`
}

type Account struct {
	FullName     string        `json:"fullName"`
	Username     string        `json:"username"`
	Password     string        `json:"password"`
	Balance      float64       `json:"balance"`
	LastLogin    time.Time     `json:"lastLogin"`
	Transactions []Transaction `json:"transactions"`
}

var bank = map[string]*Account{}

var (
	db                *os.File
	dbErr             error
	currentAccount    *Account
	previousLastLogin time.Time
)

func loadData() {
	// set a manual lock on db
	f, err := os.OpenFile("db.json.lock", os.O_CREATE|os.O_EXCL, 0)
	if err != nil {
		if os.IsExist(err) {
			panic("database lock present, maybe an instance is already running?")
		}
		panic(err)
	}
	_ = f.Close()

	db, dbErr = os.OpenFile("db.json", os.O_RDWR|os.O_CREATE, 0600)
	if dbErr != nil {
		panic(dbErr)
	}
	decode := json.NewDecoder(db)
	err = decode.Decode(&bank)
	if err != nil {
		fmt.Println("error while decoding json", err)
	}
}

func saveData() {
	data, err := json.MarshalIndent(bank, "", "\t")
	//fmt.Println(string(data))
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	err = db.Truncate(0)
	_, err = db.Seek(0, io.SeekStart)
	_, err = db.Write(data)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	_ = db.Close()
	_ = os.Remove("db.json.lock")
}

func readString(message string) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(message)
		input, err := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if err != nil || input == "" {
			fmt.Println("ERROR: invalid input")
			continue
		}
		return input
	}
}

func readYesNo(message string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(message + "[y/N] ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("ERROR: invalid input")
			continue
		}
		input = strings.TrimSpace(input)
		input = strings.ToLower(input)
		return input == "y"
	}
}

func readAmount() float64 {
	for {
		input := readString("Enter amount: ")
		amount, err := strconv.ParseFloat(input, 64)
		if err != nil {
			fmt.Println("ERROR: invalid input, enter a valid number")
			continue
		}
		if amount <= 0 {
			fmt.Println("ERROR: invalid input, amount should be greater than 0")
			continue
		}
		return amount
	}
}

func printSelectMenu(options []string) {
	fmt.Println("Select:")
	for i, option := range options {
		fmt.Printf("  %d. %s\n", i+1, option)
	}
}

func hash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	sha := base64.URLEncoding.EncodeToString(h.Sum(nil))
	return sha
}

func login() error {
	username := readString("Enter username: ")
	account, ok := bank[username]
	if !ok {
		return errors.New("user not found")
	}
	incorrectPasswordCount := 0
	for {
		password := readString("Enter password: ")
		if hash(password) != account.Password {
			if incorrectPasswordCount < 3 {
				fmt.Println("Incorrect Password")
				incorrectPasswordCount++
				continue
			} else {
				return errors.New("exceeded maximum number of login attempts")
			}
		}
		break
	}
	previousLastLogin = account.LastLogin
	account.LastLogin = time.Now()
	currentAccount = account
	fmt.Println("Hi, ", account.FullName)
	return nil
}

func createAccount() {
	username := readString("Enter username: ")

	// sanity check
	_, ok := bank[username]
	if ok {
		fmt.Println("ERROR: account already taken")
		return
	}

	account := &Account{
		Username: username,
		Balance:  1000, // joining bonus
	}

	account.FullName = readString("Enter your full name: ")

	var p1, p2 string
	for {
		p1 = readString("Enter new password: ")
		p2 = readString("Renter password: ")
		if p1 == p2 {
			break
		}
		fmt.Println("ERROR: passwords do not match")
	}
	account.Password = hash(p1)
	bank[username] = account
	fmt.Println("Account created successfully")
	return
}

func listTransactions(start int, end int) {
	transactions := currentAccount.Transactions
	l := len(transactions)
	if end == 0 {
		end = l
	}

	for i := start; i < end; i++ {
		if i == l {
			break
		}
		fmt.Println(transactions[i]) //todo pretty print?
	}
}

func checkAccountInfo() {
	fmt.Println("Name: ", currentAccount.FullName)
	fmt.Println("Username: ", currentAccount.Username)
	fmt.Println("Current account balance: ", currentAccount.Balance)
	if !previousLastLogin.IsZero() {
		fmt.Println("Last login: ", previousLastLogin.Format("2006-01-02 15:04:05"))
	}
	fmt.Println("Last 5 transactions: ")
	listTransactions(0, 5)
}

func depositCash() {
	amount := readAmount()
	currentAccount.Balance += amount
	currentAccount.Transactions = append([]Transaction{{
		Time:           time.Now(),
		Amount:         amount,
		ClosingBalance: currentAccount.Balance,
		Message:        "credited via cash deposit",
		Type:           "C",
	}}, currentAccount.Transactions...)
	fmt.Printf("%.2f successfully deposited to your account\n", amount)
	fmt.Println("Closing balance: ", currentAccount.Balance)
}

func withdrawCash() {
	amount := readAmount()
	currentAccount.Balance -= amount
	currentAccount.Transactions = append([]Transaction{{
		Time:           time.Now(),
		Amount:         amount,
		ClosingBalance: currentAccount.Balance,
		Message:        "debited via cash withdrawal",
		Type:           "D",
	}}, currentAccount.Transactions...)
	fmt.Printf("%.2f successfully withdrawn from your account\n", amount)
	fmt.Println("Closing balance: ", currentAccount.Balance)
}

func transferMoney() {
	r := readString("Enter receiver's username:")

	receiver, ok := bank[r]
	if !ok {
		fmt.Println("ERROR: receiver not found")
		return
	}

	amount := readAmount()
	if amount < 0 {
		fmt.Println("ERROR: sending amount should be positive")
		return
	}
	if currentAccount.Balance < amount {
		fmt.Println("ERROR: insufficient funds")
		return
	}

	currentTime := time.Now()
	currentAccount.Balance -= amount
	receiver.Balance += amount
	currentAccount.Transactions = append([]Transaction{{
		Time:           currentTime,
		Counterparty:   receiver.Username,
		Amount:         amount,
		ClosingBalance: currentAccount.Balance,
		Message:        fmt.Sprintf("transferred to %s", receiver.Username),
		Type:           "D",
	}}, currentAccount.Transactions...)
	receiver.Transactions = append([]Transaction{{
		Time:           currentTime,
		Counterparty:   currentAccount.Username,
		Amount:         amount,
		ClosingBalance: receiver.Balance,
		Message:        fmt.Sprintf("received from %s", currentAccount.Username),
		Type:           "C",
	}}, receiver.Transactions...)

	fmt.Printf("%.2f successfully sent to %s\n", amount, receiver.Username)
	fmt.Println("Closing balance: ", currentAccount.Balance)
}

func deleteAccount() (d bool) {
	if yes1 := readYesNo("Do you want to delete your account?: "); !yes1 {
		return
	}
	if yes2 := readYesNo("Are you sure you want to delete your account?: "); !yes2 {
		return
	}
	delete(bank, currentAccount.Username)
	return true
}

func accountLoop() {
	err := login()
	if err != nil {
		fmt.Println("ERROR: ", err)
		return
	}
	checkAccountInfo()

	printSelectMenu([]string{
		"Check account info",
		"List all transactions",
		"Deposit cash",
		"Withdraw cash",
		"Transfer money",
		"Delete account",
		"Logout",
	})

	for {
		input := readString(currentAccount.Username + "> ")
		switch input {
		case "1":
			checkAccountInfo()
		case "2":
			listTransactions(0, 0)
		case "3":
			depositCash()
		case "4":
			withdrawCash()
		case "5":
			transferMoney()
		case "6":
			if ok := deleteAccount(); ok {
				return
			}
		case "7":
			return
		default:
			fmt.Println("Enter a valid choice")
			continue
		}
	}
}

func main() {
	loadData()

	// catch keyboard interrupt to save data before exit
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		saveData()
		os.Exit(1)
	}()

	fmt.Printf("Welcome\n-------\n\n")

	printSelectMenu([]string{
		"Login",
		"Create new account",
		"Exit",
	})

	for {
		// todo create helper if possible
		input := readString("> ")
		switch input {
		case "1":
			accountLoop()
		case "2":
			createAccount()
		case "3":
			saveData()
			os.Exit(0)
		default:
			fmt.Println("Enter a valid choice")
			continue
		}
	}

	//todo convert error print statements to log
}
