package session

import (
	"fmt"
	"github.com/satori/go.uuid"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	CookieName = "goodsCool"
	Timeout    = 60 * 30
)

var Cmgr *ConversationManager

func init() {
	Cmgr = &ConversationManager{
		CookieName: CookieName,
		MaxAge:     Timeout,
		Storage:    MemStorage{DataStorage: make(map[string]interface{}, 0)},
	}
	go Cmgr.GC()
}

type MemStorage struct {
	DataStorage   map[string]interface{}
	Lock          sync.Mutex
	ReadWriteLock sync.RWMutex
}

func (ms *MemStorage) Store(key string, value interface{}) bool {
	ms.Lock.Lock()
	storage := ms.DataStorage
	storage[key] = value
	ms.Lock.Unlock()
	return true
}
func (ms *MemStorage) Load(key string) (value interface{}, ok bool) {
	ms.ReadWriteLock.RLock()
	storage := ms.DataStorage
	value = storage[key]
	if value != nil {
		ok = true
	} else {
		ok = false
	}

	ms.ReadWriteLock.RUnlock()
	return
}
func (ms *MemStorage) Delete(key string) (value interface{}) {
	ms.Lock.Lock()
	storage := ms.DataStorage
	if v, ok := storage[key]; ok {
		value = v
	}
	delete(storage, key)
	ms.Lock.Unlock()
	return
}

func (ms *MemStorage) Range() {
	for key, value := range ms.DataStorage {
		fmt.Printf(" 遍历 %v,%v\n", key, value)
	}
}

type IConversationManager interface {
	Create(cid string) (conversation *Conversation, err error)
	Destroy(cid string) (b bool, err error)
	GC()
}

type IConversation interface {
	Get(key interface{}) (info interface{}, err error)
	Set(key interface{}, value interface{}) (b bool, err error)
}

type Conversation struct {
	LastAccessTime time.Time
	Cid            string
	CSData         MemStorage
	MaxAge         int64
}
type ConversationManager struct {
	CookieName string
	MaxAge     int64
	Storage    MemStorage
}

//实现 Create / Distroy / Get / Set 方法
//创建会话
func (cm *ConversationManager) Create(cid string) (*Conversation, error) {
	//根据cid 创建一个conversatioin
	storage := cm.Storage
	value, ok := storage.Load(cid)
	if ok {
		conversation := value.(*Conversation)
		return conversation, nil
	}

	m := MemStorage{DataStorage: make(map[string]interface{}, 0)}
	c2 := &Conversation{
		LastAccessTime: time.Now(),
		Cid:            cid,
		CSData:         m,
		MaxAge:         60 * 30,
	}

	cm.Storage.Store(cid, c2)
	return c2, nil
}

//getConversation
func (cm *ConversationManager) GetConversation(cid string) (conversation *Conversation) {
	storage := cm.Storage
	value, ok := storage.Load(cid)
	if ok {
		conversation = value.(*Conversation)
		return conversation
	}
	return conversation
}

// 销毁cid 对应的会话
func (cm *ConversationManager) Destroy(cid string) (b bool, err error) {
	if _, ok := cm.Storage.Load(cid); ok {
		cm.Storage.Delete(cid)
	}
	return true, nil
}

// 会话域存储Get
func (c *Conversation) Get(key string) (interface{}, error) {
	//log.Println("从", c.Cid, "中取数据：key=", key)
	if load, ok := c.CSData.Load(key); ok {
		return load, nil
	} else {
		return nil, nil
	}
}

// 会话域存储Set
func (c *Conversation) Set(key string, value interface{}) (b bool, err error) {
	c.CSData.Store(key, value)
	return true, nil
}

// ConversationManager GC
func (cm *ConversationManager) GC() {
	for {
		time.Sleep(time.Second * 10)
		log.Println("GC ing ...")
		storage := cm.Storage.DataStorage
		duration := time.Duration(cm.MaxAge * 1000 * 1000 * 1000)
		now := time.Now()
		for cid, conversation := range storage {
			//log.Printf("GC 遍历：%v,%v\n", cid, conversation)
			if c, ok := conversation.(*Conversation); ok {
				expire := c.LastAccessTime.Add(duration)
				//log.Println("如果 expire 大于当前时间:", expire, " 当前时间：now=", now)
				if now.After(expire) {
					//将conversation销毁
					log.Println("销毁Conversation: Cid=", cid)
					cm.Destroy(cid)
				}
			}
		}
	}
}

// 给普通链接处理会话，没有就创建同时处理客户端
//的cookie
func (cm *ConversationManager) ManagerNormalRequest(w http.ResponseWriter, r *http.Request) (conversation *Conversation, e error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		e = err
	}
	var conversationId string
	if cookie == nil {
		conversationId, err = url.QueryUnescape(uuid.Must(uuid.NewV4(), nil).String())
		if err != nil {
			e = err
			return
		}
	} else {
		conversationId = cookie.Value
	}
	//创建Conversation,如果有就返回
	cs, err := cm.Create(conversationId)
	if err != nil {
		e = err
		return
	}
	cs.LastAccessTime = time.Now()
	expire := cs.LastAccessTime.Add(time.Duration(cm.MaxAge * 1000 * 1000 * 1000))
	//log.Println("expire: ", expire)
	//设置cookie
	c := http.Cookie{
		Name:  CookieName,
		Value: conversationId,
		Path:  "/",
		//Domain:     "",
		Expires:    expire,
		RawExpires: "",
		MaxAge:     int(cm.MaxAge),
		Secure:     false,
		HttpOnly:   false,
		SameSite:   0,
		Raw:        "",
		Unparsed:   nil,
	}
	//log.Println(c)
	http.SetCookie(w, &c)
	return cs, e
}

func UUID() (string, error) {
	conversationId, err := url.QueryUnescape(uuid.Must(uuid.NewV4(), nil).String())
	return conversationId, err
}

func (cm *ConversationManager) ManagerLogin(loginUser interface{}, w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(CookieName)
	//获取当前时间
	now := time.Now()
	var conversationId string
	if cookie != nil {
		//log.Println("已经有了cookie ==>", cookie)
		conversationId = cookie.Value
	} else {
		conversationId, _ = UUID()
		//log.Println("没有cookie,创建会话 cid=", conversationId)
	}
	expire := now.Add(time.Duration(Cmgr.MaxAge * 1000 * 1000 * 1000))
	//设置cookie
	cookie2 := http.Cookie{
		Name:  CookieName,
		Value: conversationId,
		Path:  "/",
		//	Domain:     "",
		Expires:    expire,
		RawExpires: "",
		MaxAge:     0,
		Secure:     false,
		HttpOnly:   false,
		SameSite:   0,
		Raw:        "",
		Unparsed:   nil,
	}
	//存放登录数据
	var conversation *Conversation
	conversation = Cmgr.GetConversation(conversationId)
	if conversation == nil {
		create, _ := Cmgr.Create(cookie2.Value)
		conversation = create
	}
	conversation.LastAccessTime = now
	conversation.Set("user", loginUser)
	log.Println("存放了User: conversationId:", conversation.Cid, "存放的用户：", loginUser)
	http.SetCookie(w, &cookie2)
}
