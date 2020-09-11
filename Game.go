package main

import (
	"container/list"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/umpc/go-sortedmap"
	"log"
	"strconv"
	"time"
)

var GameMap = NewMap(100)
var connections = sortedmap.New(10, isLesserThan)
var ticker = time.NewTicker(5 * time.Millisecond)

const FIELD_SIZE = 50
const STEP_SIZE = 10

type Bomberman struct {
	UserID         uint64
	PositionX      int
	PositionY      int
	Name           string
	oldPositionX   int
	oldPositionY   int
	lastBombPlaced time.Time
	BombRadius     int
	bombTime       int
	isAlive        bool
}

func (r *Bomberman) String() string {
	return "Bomberman: {" + strconv.FormatUint(r.UserID, 10) + " | " + strconv.FormatInt(int64(r.PositionX), 10) + " | " + strconv.FormatInt(int64(r.PositionY), 10) + " | " + r.lastBombPlaced.String() + "}"
}

func NewBomberman(userID uint64, positionX int, positionY int, name string) *Bomberman {
	return &Bomberman{
		UserID:       userID,
		PositionX:    positionX,
		PositionY:    positionY,
		oldPositionX: positionX,
		oldPositionY: positionY,
		Name:         name,
		BombRadius:   3,
		bombTime:     3,
		isAlive:      true,
	}
}

func (r *Bomberman) placeBomb() {
	bomb := NewBomb(r)
	GameMap.Fields[r.PositionX][r.PositionY].addBomb(&bomb)
	bomb.startBomb(r.PositionX, r.PositionY)
}

//Wrapper for the user
type Session struct {
	User              *User           //Connected user
	Bomber            *Bomberman      //Bomber of the connected user
	Connection        *websocket.Conn //Websocket connection
	ConnectionStarted time.Time       //point when player joined
}

func NewSession(user *User, character *Bomberman, connection *websocket.Conn, connectionStarted time.Time) *Session {
	return &Session{User: user, Bomber: character, Connection: connection, ConnectionStarted: connectionStarted}
}

//Returns the string representation of the connection
func (r *Session) String() string {
	return "Session: { " + r.User.String() + "|" + r.Bomber.String() + "|" + r.Connection.RemoteAddr().String() + "|" + r.ConnectionStarted.String() + "}"
}

//Prints every active connection
func AllConnectionsAsString() string {
	result := "Active Connections:"

	iterCh, err := connections.IterCh()

	if err != nil {
		log.Println(err)
		return result
	}
	defer iterCh.Close()

	for v := range iterCh.Records() {
		result += v.Val.(*Session).String() + "\n"
	}
	return result
}

//Starts the interaction loop
func StartPlayerLoop(session *Session) {
	//Add the infos to the connection map
	connections.Insert(session.User.UserID, session)

	playerWebsocketLoop(session)
	//Remove from the connection map
	connections.Delete(session.User.UserID)
}

//interaction loop
func playerWebsocketLoop(session *Session) {
	for {
		_, p, err := session.Connection.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}

		switch string(p) {
		//W
		case "w":
			if session.Bomber.canEnter(session.Bomber.PositionX, session.Bomber.PositionY-STEP_SIZE) {
				session.Bomber.PositionY -= STEP_SIZE
			}

		//A
		case "a":
			if session.Bomber.canEnter(session.Bomber.PositionX-STEP_SIZE, session.Bomber.PositionY) {
				session.Bomber.PositionX -= STEP_SIZE
			}

		//S
		case "s":
			if session.Bomber.canEnter(session.Bomber.PositionX, session.Bomber.PositionY+STEP_SIZE) {
				session.Bomber.PositionY += STEP_SIZE
			}

		//D
		case "d":
			if session.Bomber.canEnter(session.Bomber.PositionX+STEP_SIZE, session.Bomber.PositionY) {
				session.Bomber.PositionX += STEP_SIZE
			}
		//Spacebar
		case "space":
			go session.Bomber.placeBomb()

		default:
			break
		}
		updatePlayerPositioning(session)
	}

}
func updatePlayerPositioning(session *Session) {
	posY := session.Bomber.PositionX / FIELD_SIZE
	posX := session.Bomber.PositionY / FIELD_SIZE
	oldPosX := session.Bomber.oldPositionX / FIELD_SIZE
	oldPosY := session.Bomber.oldPositionY / FIELD_SIZE
	if posX != oldPosX {
		removePlayerFromList(GameMap.Fields[oldPosX][posY].Player, session.Bomber)
		GameMap.Fields[posX][posY].Player.PushBack(&list.Element{Value: session.Bomber})
	} else if posY != oldPosY {
		removePlayerFromList(GameMap.Fields[posX][oldPosY].Player, session.Bomber)
		GameMap.Fields[posX][posY].Player.PushBack(&list.Element{Value: session.Bomber})
	}
}

func removePlayerFromList(l *list.List, b *Bomberman) {
	element := l.Front()
	if element != nil {
		if element.Value.(*Bomberman).UserID == b.UserID {
			l.Remove(element)
			return
		}
		for element.Next() != nil {
			element = element.Next()
			if element.Value.(*Bomberman).UserID == b.UserID {
				l.Remove(element)
				return
			}
		}
	}
	log.Println("Player not found in list")
}

func (r *Bomberman) canEnter(x int, y int) bool {
	arrayPosX := x / FIELD_SIZE
	arrayPosY := y / FIELD_SIZE
	inBounds := arrayPosX >= 0 && arrayPosY >= 0 && arrayPosX < len(GameMap.Fields) && arrayPosY < len(GameMap.Fields[arrayPosX])
	isAccessible := GameMap.Fields[arrayPosX][arrayPosY].Contains[0].isAccessible() && GameMap.Fields[arrayPosX][arrayPosY].Contains[1].isAccessible()
	return inBounds && isAccessible
}

func UpdateClients() {
	for _ = range ticker.C {
		err := sendDataToClients()
		if err != nil {
			log.Println(err)
			break
		}
	}
	log.Println("Updating Clients stopped.")
}

func sendDataToClients() error {
	//collect data
	sessions := make([]Bomberman, connections.Len())
	count := 0

	iterCh, err := connections.IterCh()

	if err != nil {
		return nil
	}
	defer iterCh.Close()

	for v := range iterCh.Records() {
		sessions[count] = *v.Val.(*Session).Bomber
		count++
	}

	jsonBytes, err := json.MarshalIndent(sessions, "", " ")
	if err != nil {

		return err
	}
	iterCh, err = connections.IterCh()

	if err != nil {
		return nil
	}

	for v := range iterCh.Records() {

		if err := v.Val.(*Session).Connection.WriteMessage(websocket.TextMessage, jsonBytes); err != nil {
			return err
		}
	}

	return nil
}

func isLesserThan(a interface{}, b interface{}) bool {
	return a.(*Session).User.UserID < b.(*Session).User.UserID
}
