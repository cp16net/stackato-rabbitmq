package rabbitmq

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"os"

	"github.com/cloudfoundry-community/go-cfenv"
	"github.com/streadway/amqp"

	"github.com/cp16net/stackato-rabbitmq/common"
)

// User db Model
type User struct {
	Username string `json:"user"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

var amqpuri string

func dbConnection() (*amqp.Connection, error) {
	if os.Getenv("VCAP_SERVICES") != "" {
		appEnv, err := cfenv.Current()
		if err != nil {
			errstr := "Failed to get the current cf application environment: " + err.Error()
			return nil, errors.New(errstr)
		}
		svc, err := appEnv.Services.WithName("stackato-rabbitmq")
		if err != nil {
			errstr := "Failed to get the application environment service: " + err.Error()
			return nil, errors.New(errstr)
		}
		uri, ok := svc.CredentialString("uri")
		if !ok {
			errstr := "Failed to get the rabbitmq uri connection string: " + err.Error()
			return nil, errors.New(errstr)
		}
		amqpuri = uri
	} else {
		amqpuri = "amqp://user:password@localhost:5672/"
	}

	common.Logger.Debug("RUNNING IN CF MODE WITH RABBITMQ")

	conn, err := amqp.Dial(amqpuri)
	if err != nil {
		errstr := "failed to connect rabbitmq: " + err.Error()
		return nil, errors.New(errstr)
	}
	return conn, nil
}

func closeConnection(db *amqp.Connection) {
	db.Close()
}

func init() {
	db, err := dbConnection()
	if err != nil {
		common.Logger.Fatal(err)
	}
	defer closeConnection(db)
}

func generateString(length int, characters string) (string, error) {
	b := make([]byte, length)
	max := big.NewInt(int64(len(characters)))
	for i := range b {
		var c byte
		rint, err := rand.Int(rand.Reader, max)
		if err != nil {
			common.Logger.Error(err)
			return "", errors.New("Unable to generate a string. Error: " + err.Error())
		}
		c = characters[rint.Int64()]
		b[i] = c
	}
	common.Logger.Debug("generated string: ", string(b))
	return string(b), nil
}

const usercharacters = "abcdefghijklmnopqrstuvwxyz"
const passwordcharacters = `abcdefghijklmnopqrstuvwxyz1234567890`
const queueName = "users"

// Write generates a random user
func Write() (*User, error) {
	db, err := dbConnection()
	if err != nil {
		common.Logger.Fatal(err)
		return nil, err
	}
	defer closeConnection(db)
	username, err := generateString(10, usercharacters)
	if err != nil {
		common.Logger.Error("Error generating a username: ", err)
		return nil, err
	}
	password, err := generateString(10, passwordcharacters)
	if err != nil {
		common.Logger.Error("Error generating a password: ", err)
		return nil, err
	}
	var user = User{
		Username: username,
		Password: password,
		Email:    username + "@gmail.com",
	}
	bytes, err := json.MarshalIndent(user, "", "\t")
	if err != nil {
		common.Logger.Error("Error marshalling the user object to json: ", err)
		return nil, err
	}

	ch, err := db.Channel()
	if err != nil {
		common.Logger.Error("Error getting a channel on rabbitmq: ", err)
		return nil, err
	}
	defer ch.Close()
	q, err := ch.QueueDeclare(
		queueName, // name
		false,     // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		common.Logger.Error("Error declaring queue on rabbitmq: ", err)
		return nil, err
	}
	err = ch.Publish(
		"",     // exchange
		q.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        bytes,
		})
	return &user, nil
}

// Read gets the data from the database and returns it
func Read() ([]User, error) {
	db, err := dbConnection()
	if err != nil {
		common.Logger.Fatal(err)
		return nil, err
	}
	defer closeConnection(db)

	ch, err := db.Channel()
	if err != nil {
		common.Logger.Error("Error getting a channel on rabbitmq: ", err)
		return nil, err
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		queueName, // name
		false,     // durable
		false,     // delete when usused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		common.Logger.Error("Error declaring queue on rabbitmq: ", err)
		return nil, err
	}

	users := []User{}
	for {
		d, _, getErr := ch.Get(q.Name, true)
		if getErr != nil {
			common.Logger.Error("Error getting message from rabbitmq: ", err)
			return nil, err
		}
		if d.Body == nil {
			break
		}
		common.Logger.Debugf("Received a message: %s", d.Body)
		var user User
		err = json.Unmarshal(d.Body, &user)
		if err != nil {
			common.Logger.Error("Error unmarshalling message received to user object: ", err)
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}
