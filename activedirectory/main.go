package main

import (
	"fmt"
	"log"
	"os"
	"github.com/go-ldap/ldap"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <DC>\n", os.Args[0])
		os.Exit(1)
	}

	server := os.Args[1]
	ldapURL := fmt.Sprintf("ldap://%s/", server)
	conn, err := ldap.DialURL(ldapURL)
	if err != nil {
			log.Fatal(err)
	}
	defer conn.Close()

	err = conn.Bind()
	if err != nil {
		log.Fatal(err)
	}

	searchRequest := ldap.NewSearchRequest(
		"", // The base dn to search
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(&(samAccountType=805306368))", // The filter to apply
		nil,                    // A list attributes to retrieve
		nil,
	)
	
	sr, err := conn.Search(searchRequest)
	if err != nil {
		log.Fatal(err)
	}
	
	for _, entry := range sr.Entries {
		fmt.Printf("%s: %v\n", entry.DN, entry.GetAttributeValue("cn"))
	}
}