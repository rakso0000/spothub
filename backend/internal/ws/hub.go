package ws

import "sync"

type Client struct {
	SpotID int64
	UserID int64
	Nick   string
	Send   chan []byte
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[int64]map[*Client]struct{} // spotID -> клиенты
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[int64]map[*Client]struct{})}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[c.SpotID] == nil {
		h.rooms[c.SpotID] = make(map[*Client]struct{})
	}
	h.rooms[c.SpotID][c] = struct{}{}
}

// Unregister безопасно вызывать несколько раз.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[c.SpotID]
	if _, ok := room[c]; !ok {
		return
	}
	delete(room, c)
	close(c.Send)
	if len(room) == 0 {
		delete(h.rooms, c.SpotID)
	}
}

// Broadcast рассылает сообщение всем в комнате.
// Медленных клиентов отключаем, чтобы они не тормозили остальных.
func (h *Hub) Broadcast(spotID int64, msg []byte) {
	h.mu.RLock()
	var slow []*Client
	for c := range h.rooms[spotID] {
		select {
		case c.Send <- msg:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		h.Unregister(c)
	}
}

// SendTo отправляет сообщение одному клиенту (например, ошибку).
func (h *Hub) SendTo(c *Client, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.rooms[c.SpotID][c]; !ok {
		return
	}
	select {
	case c.Send <- msg:
	default:
	}
}

// KickUser отключает все соединения пользователя (например, при бане).
func (h *Hub) KickUser(userID int64) {
	h.mu.RLock()
	var targets []*Client
	for _, room := range h.rooms {
		for c := range room {
			if c.UserID == userID {
				targets = append(targets, c)
			}
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		h.Unregister(c)
	}
}
