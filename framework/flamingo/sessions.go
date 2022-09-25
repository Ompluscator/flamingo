package flamingo

import (
	"net/http"
	"net/url"
	"os"

	"flamingo.me/dingo"
	"flamingo.me/flamingo/v3/core/healthcheck/domain/healthcheck"
	sessionhealthcheck "flamingo.me/flamingo/v3/framework/flamingo/healthcheck"
	"github.com/boj/redistore"
	"github.com/gomodule/redigo/redis"
	"github.com/gorilla/sessions"
	"github.com/zemirco/memorystore"
)

// SessionModule for session management
type SessionModule struct {
	backend              string
	secret               string
	fileName             string
	secure               bool
	sameSite             string
	storeLength          int
	maxAge               int
	path                 string
	redisHost            string
	redisPassword        string
	redisIdleConnections int
	redisMaxAge          int
	redisDatabase        string
	healthcheckSession   bool
}

// Inject dependencies
func (m *SessionModule) Inject(config *struct {
	// session config is optional to allow usage of the DefaultConfig
	Backend  string `inject:"config:session.backend"`
	Secret   string `inject:"config:session.secret"`
	FileName string `inject:"config:session.file,optional"`
	Secure   bool   `inject:"config:session.cookie.secure"`
	SameSite string `inject:"config:session.cookie.sameSite"`
	// float64 is used due to the injection as config from json - int is not possible on this
	StoreLength          float64 `inject:"config:session.store.length,optional"`
	MaxAge               float64 `inject:"config:session.max.age"`
	Path                 string  `inject:"config:session.cookie.path"`
	RedisURL             string  `inject:"config:session.redis.url,optional"`
	RedisHost            string  `inject:"config:session.redis.host,optional"`
	RedisPassword        string  `inject:"config:session.redis.password,optional"`
	RedisIdleConnections float64 `inject:"config:session.redis.idle.connections,optional"`
	RedisMaxAge          float64 `inject:"config:session.redis.maxAge,optional"`
	RedisDatabase        string  `inject:"config:session.redis.database,optional"`
	CheckSession         bool    `inject:"config:session.healthcheck,optional"`
}) {
	m.backend = config.Backend
	m.secret = config.Secret
	m.fileName = config.FileName
	m.secure = config.Secure
	m.sameSite = config.SameSite
	m.storeLength = int(config.StoreLength)
	m.maxAge = int(config.MaxAge)
	m.path = config.Path
	m.redisHost, m.redisPassword = getRedisConnectionInformation(config.RedisURL, config.RedisHost, config.RedisPassword)
	m.redisIdleConnections = int(config.RedisIdleConnections)
	m.redisDatabase = config.RedisDatabase
	m.maxAge = int(config.MaxAge)
	m.healthcheckSession = config.CheckSession
}

// Configure DI
func (m *SessionModule) Configure(injector *dingo.Injector) {
	switch m.backend {
	case "redis":
		var sessionStore *redistore.RediStore
		var err error

		if m.redisDatabase != "" {
			sessionStore, err = redistore.NewRediStoreWithDB(int(m.redisIdleConnections), "tcp", m.redisHost, m.redisPassword, m.redisDatabase, []byte(m.secret))
		} else {
			sessionStore, err = redistore.NewRediStore(int(m.redisIdleConnections), "tcp", m.redisHost, m.redisPassword, []byte(m.secret))
		}

		if err != nil {
			panic(err) // todo: don't panic? fallback?
		}

		sessionStore.SetMaxAge(m.maxAge)
		sessionStore.SetMaxLength(m.storeLength)
		sessionStore.DefaultMaxAge = m.redisMaxAge
		m.setSessionstoreOptions(sessionStore.Options)

		injector.Bind(new(sessions.Store)).ToInstance(sessionStore)
		injector.Bind(new(redis.Pool)).ToInstance(sessionStore.Pool)

		if m.healthcheckSession {
			injector.BindMap(new(healthcheck.Status), "session").To(sessionhealthcheck.RedisSession{})
		}
	case "file":
		os.Mkdir(m.fileName, os.ModePerm)
		sessionStore := sessions.NewFilesystemStore(m.fileName, []byte(m.secret))

		sessionStore.MaxLength(m.storeLength)
		sessionStore.MaxAge(m.maxAge)
		m.setSessionstoreOptions(sessionStore.Options)

		injector.Bind(new(sessions.Store)).ToInstance(sessionStore)

		if m.healthcheckSession {
			injector.BindMap(new(healthcheck.Status), "session").To(sessionhealthcheck.FileSession{})
		}
	default: //memory
		sessionStore := memorystore.NewMemoryStore([]byte(m.secret))

		sessionStore.MaxLength(m.storeLength)
		sessionStore.MaxAge(m.maxAge)
		m.setSessionstoreOptions(sessionStore.Options)

		injector.Bind(new(sessions.Store)).ToInstance(sessionStore)

		if m.healthcheckSession {
			injector.BindMap(new(healthcheck.Status), "session").To(healthcheck.Nil{})
		}
	}
}

func (m *SessionModule) setSessionstoreOptions(options *sessions.Options) {
	options.Domain = ""
	options.Path = m.path
	options.MaxAge = m.maxAge
	options.Secure = m.secure
	options.HttpOnly = true
	switch m.sameSite {
	case "strict":
		options.SameSite = http.SameSiteStrictMode
	case "none":
		options.SameSite = http.SameSiteNoneMode
	case "lax":
		options.SameSite = http.SameSiteLaxMode
	default:
		options.SameSite = http.SameSiteDefaultMode
	}
}

func getRedisConnectionInformation(redisURL, redisHost, redisPassword string) (string, string) {
	if redisURL != "" {
		parsedRedisURL, err := url.Parse(redisURL)
		if err != nil {
			return redisHost, redisPassword
		}
		redisHostFromURL := parsedRedisURL.Host
		if redisHostFromURL != "" {
			redisHost = redisHostFromURL
		}
		redisPasswordFromURL, isRedisPasswordInURL := parsedRedisURL.User.Password()
		if isRedisPasswordInURL {
			redisPassword = redisPasswordFromURL
		}
	}

	return redisHost, redisPassword
}
