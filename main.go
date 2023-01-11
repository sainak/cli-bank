package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Account struct {
	ID        uint      `json:"ID" gorm:"primaryKey"`
	FullName  string    `json:"fullName"`
	Username  string    `json:"username" gorm:"unique;index"`
	Password  string    `json:"password"`
	Balance   float64   `json:"balance"`
	LastLogin time.Time `json:"lastLogin"`
	CreatedAt time.Time
}

type Transaction struct {
	ID             uint      `json:"ID" gorm:"primaryKey"`
	Time           time.Time `json:"time"`
	Counterparty   string    `json:"counterparty"`
	Amount         float64   `json:"amount"`
	ClosingBalance float64   `json:"closingBalance"`
	Message        string    `json:"message"`
	Type           string    `json:"type"`
	AccountID      uint
	//Account        Account `gorm:"foreignKey:AccountID"`
}

var (
	db                *gorm.DB
	dbErr             error
	currentAccount    Account
	previousLastLogin time.Time
)

// helper to read string with spaces from the stdin buffer
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

// helper to ask for decisions
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

// helper to read positive float64 values from stdin buffer
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
	var account Account
	err := db.Where("username = ?", username).First(&account).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("user not found")
		}
		log.Fatalln(err)
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
	err = db.Save(&account).Error
	if err != nil {
		log.Fatalln(err)
	}
	currentAccount = account
	fmt.Println("Hi, ", account.FullName)
	return nil
}

func createAccount() {
	username := readString("Enter username: ")

	// sanity check
	var exists bool
	err := db.Model(Account{}).
		Select("count(*) > 0").
		Where("username = ?", username).
		Find(&exists).Error
	if err != nil {
		log.Fatalln(err)
	}
	if exists {
		fmt.Println("ERROR: account already taken")
		return
	}

	account := Account{
		Username: username,
		Balance:  1000, // joining amount
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
	err = db.Create(&account).Error
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println("Account created successfully")
	return
}

func listTransactions(start int, end int) {
	var transactions []Transaction
	err := db.Where("account_id = ?", currentAccount.ID).Find(&transactions).Error
	if err != nil {
		log.Fatalln(err)
	}
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
	err := db.Transaction(func(tx *gorm.DB) error {
		currentAccount.Balance += amount
		if err := tx.Save(&currentAccount).Error; err != nil {
			return err
		}
		if err := tx.Create(&Transaction{
			Time:           time.Now(),
			Amount:         amount,
			ClosingBalance: currentAccount.Balance,
			Message:        "credited via cash deposit",
			Type:           "C",
			AccountID:      currentAccount.ID,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Printf("%.2f successfully deposited to your account\n", amount)
	fmt.Println("Closing balance: ", currentAccount.Balance)
}

func withdrawCash() {
	amount := readAmount()
	err := db.Transaction(func(tx *gorm.DB) error {
		currentAccount.Balance -= amount
		if err := tx.Save(&currentAccount).Error; err != nil {
			return err
		}
		if err := tx.Create(&Transaction{
			Time:           time.Now(),
			Amount:         amount,
			ClosingBalance: currentAccount.Balance,
			Message:        "debited via cash withdrawal",
			Type:           "D",
			AccountID:      currentAccount.ID,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Printf("%.2f successfully withdrawn from your account\n", amount)
	fmt.Println("Closing balance: ", currentAccount.Balance)
}

func transferMoney() {
	var receiver Account
	r := readString("Enter receiver's username:")
	result := db.Where("username = ?", r).First(&receiver)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			fmt.Println("ERROR: receiver not found")
			return
		}
		log.Fatalln(result.Error)
	}

	amount := readAmount()
	if currentAccount.Balance < amount {
		fmt.Println("ERROR: insufficient funds")
		return
	}

	currentTime := time.Now()

	err := db.Transaction(func(tx *gorm.DB) error {
		currentAccount.Balance -= amount
		receiver.Balance += amount
		if err := tx.Save(&currentAccount).Error; err != nil {
			return err
		}
		if err := tx.Save(&receiver).Error; err != nil {
			return err
		}
		if err := tx.Create(&Transaction{
			Time:           currentTime,
			Counterparty:   receiver.Username,
			Amount:         amount,
			ClosingBalance: currentAccount.Balance,
			Message:        fmt.Sprintf("transferred to %s", receiver.Username),
			Type:           "D",
			AccountID:      currentAccount.ID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Transaction{
			Time:           currentTime,
			Counterparty:   currentAccount.Username,
			Amount:         amount,
			ClosingBalance: receiver.Balance,
			Message:        fmt.Sprintf("received from %s", currentAccount.Username),
			Type:           "C",
			AccountID:      receiver.ID,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
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
	err := db.Delete(&currentAccount).Error
	if err != nil {
		log.Fatalln(err)
	}
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

	db, dbErr = gorm.Open(sqlite.Open("db.sqlite"), &gorm.Config{})
	if dbErr != nil {
		panic("failed to connect database")
	}

	// Migrate the schema
	err := db.AutoMigrate(&Account{}, &Transaction{})
	if err != nil {
		return
	}

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
			os.Exit(0)
		default:
			fmt.Println("Enter a valid choice")
			continue
		}
	}
}
