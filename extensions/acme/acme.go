package acme

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	legoLog "github.com/go-acme/lego/v4/log"
	"github.com/go-acme/lego/v4/registration"
	"github.com/sagernet/sing-tools/extensions/log"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/rw"
	"github.com/sagernet/sing/common/x/list"
)

func init() {
	legoLog.Logger = log.NewLogger("acme")
}

type CertificateUpdateListener func(certificate *tls.Certificate)

type CertificateManager struct {
	email    string
	path     string
	provider string

	access     sync.Mutex
	callbacks  map[string]*list.List[CertificateUpdateListener]
	renewClose chan struct{}
}

func NewCertificateManager(settings *Settings) *CertificateManager {
	m := &CertificateManager{
		email:     settings.Email,
		path:      settings.DataDirectory,
		provider:  settings.DNSProvider,
		callbacks: make(map[string]*list.List[CertificateUpdateListener]),
	}
	if m.path == "" {
		m.path = "acme"
	}
	return m
}

func (c *CertificateManager) GetKeyPair(domain string) (*tls.Certificate, error) {
	if domain == "" {
		return nil, E.New("acme: empty domain name")
	}

	dnsProvider, err := NewDNSChallengeProviderByName(c.provider)
	if err != nil {
		return nil, err
	}

	accountPath := c.path + "/account.json"
	accountKeyPath := c.path + "/account.key"

	privateKeyPath := c.path + "/" + domain + ".key"
	certificatePath := c.path + "/" + domain + ".crt"
	requestPath := c.path + "/" + domain + ".json"

	if !rw.FileExists(accountKeyPath) {
		err = writeNewPrivateKey(accountKeyPath)
		if err != nil {
			return nil, err
		}
	}

	accountKey, err := readPrivateKey(accountKeyPath)
	if err != nil {
		return nil, err
	}

	user := &acmeUser{
		email:      c.email,
		privateKey: accountKey,
	}

	if rw.FileExists(accountPath) {
		var account registration.Resource
		err = rw.ReadJSON(accountPath, &account)
		if err != nil {
			return nil, err
		}
		user.registration = &account
	}

	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA4096

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, err
	}

	err = client.Challenge.SetDNS01Provider(dnsProvider)
	if err != nil {
		return nil, err
	}

	if user.GetRegistration() == nil {
		account, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, err
		}
		user.registration = account
		err = rw.WriteJSON(accountPath, account)
		if err != nil {
			return nil, err
		}
	}

	var renew bool
	keyPair, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err == nil {
		cert, err := x509.ParseCertificate(keyPair.Certificate[0])
		if err == nil {
			keyPair.Leaf = cert
			expiresDays := time.Until(cert.NotAfter).Hours() / 24
			if expiresDays > 15 {
				return &keyPair, nil
			} else {
				renew = true
			}
		}
	}

	if renew && rw.FileExists(requestPath) {
		var request Certificate
		err = rw.ReadJSON(requestPath, &request)
		if err != nil {
			return nil, err
		}
		newCert, err := client.Certificate.Renew((certificate.Resource)(request), true, false, "")
		if err != nil {
			return nil, err
		}
		err = rw.WriteJSON(requestPath, (*Certificate)(newCert))
		if err != nil {
			return nil, err
		}
		certResponse, err := http.Get(newCert.CertURL)
		if err != nil {
			return nil, err
		}
		defer certResponse.Body.Close()
		content, err := ioutil.ReadAll(certResponse.Body)
		if err != nil {
			return nil, err
		}
		if certResponse.StatusCode != 200 {
			return nil, E.New("HTTP ", certResponse.StatusCode, ": ", string(content))
		}
		err = rw.WriteFile(certificatePath, content)
		if err != nil {
			return nil, err
		}

		keyPair, err = tls.LoadX509KeyPair(certificatePath, privateKeyPath)
		if err != nil {
			return nil, err
		}

		goto finish
	}

	if !rw.FileExists(certificatePath) {
		if !rw.FileExists(privateKeyPath) {
			err = writeNewPrivateKey(privateKeyPath)
			if err != nil {
				return nil, err
			}
		}

		privateKey, err := readPrivateKey(privateKeyPath)
		if err != nil {
			return nil, err
		}

		request := certificate.ObtainRequest{
			Domains:    []string{domain},
			Bundle:     true,
			PrivateKey: privateKey,
		}
		certificates, err := client.Certificate.Obtain(request)
		if err != nil {
			return nil, err
		}
		err = rw.WriteJSON(requestPath, (*Certificate)(certificates))
		if err != nil {
			return nil, err
		}
		certResponse, err := http.Get(certificates.CertURL)
		if err != nil {
			return nil, err
		}
		defer certResponse.Body.Close()
		content, err := ioutil.ReadAll(certResponse.Body)
		if err != nil {
			return nil, err
		}
		if certResponse.StatusCode != 200 {
			return nil, E.New("HTTP ", certResponse.StatusCode, ": ", string(content))
		}
		err = rw.WriteFile(certificatePath, content)
		if err != nil {
			return nil, err
		}
	}

finish:
	keyPair, err = tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil {
		return nil, err
	}

	c.access.Lock()
	listeners := c.callbacks[domain]
	if listeners != nil {
		for listener := listeners.Front(); listener != nil; listener = listener.Next() {
			listener.Value(&keyPair)
		}
	}
	c.access.Unlock()

	return &keyPair, nil
}

func (c *CertificateManager) RegisterUpdateListener(domain string, listener CertificateUpdateListener) *list.Element[CertificateUpdateListener] {
	c.access.Lock()
	defer c.access.Unlock()
	listeners := c.callbacks[domain]
	if listeners != nil {
		listeners = new(list.List[CertificateUpdateListener])
		c.callbacks[domain] = listeners
	}
	element := listeners.PushBack(listener)
	if c.renewClose == nil {
		c.start()
	}
	return element
}

func (c *CertificateManager) UnregisterUpdateListener(element *list.Element[CertificateUpdateListener]) {
	c.access.Lock()
	defer c.access.Unlock()
	element.List().Remove(element)
	for _, listeners := range c.callbacks {
		if listeners.Len() > 0 {
			return
		}
	}
	renewClose := c.renewClose
	if renewClose != nil {
		close(renewClose)
		c.renewClose = nil
	}
}

func (c *CertificateManager) start() {
	renew := time.NewTicker(time.Hour * 24)
	defer renew.Stop()
	renewClose := make(chan struct{})
	c.renewClose = renewClose
	go func() {
		select {
		case <-renew.C:
			c.access.Lock()
			domains := make([]string, 0, len(c.callbacks))
			for domain := range c.callbacks {
				domains = append(domains, domain)
			}
			c.access.Unlock()
			for _, domain := range domains {
				_, _ = c.GetKeyPair(domain)
			}
		case <-renewClose:
			return
		}
	}()
}

type acmeUser struct {
	email        string
	privateKey   crypto.PrivateKey
	registration *registration.Resource
}

func (u *acmeUser) GetEmail() string {
	return u.email
}

func (u *acmeUser) GetRegistration() *registration.Resource {
	return u.registration
}

func (u *acmeUser) GetPrivateKey() crypto.PrivateKey {
	return u.privateKey
}

func readPrivateKey(path string) (crypto.PrivateKey, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return privateKey.(crypto.PrivateKey), nil
}

func writeNewPrivateKey(path string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}
	pkcsBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	return rw.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcsBytes}))
}

type Certificate struct {
	Domain            string `json:"domain"`
	CertURL           string `json:"certUrl"`
	CertStableURL     string `json:"certStableUrl"`
	PrivateKey        []byte `json:"private_key"`
	Certificate       []byte `json:"certificate"`
	IssuerCertificate []byte `json:"issuer_certificate"`
	CSR               []byte `json:"csr"`
}
