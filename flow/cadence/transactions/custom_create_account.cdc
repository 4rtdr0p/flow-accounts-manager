import Crypto

transaction(publicKeys: [Crypto.KeyListEntry], contracts: {String: String}) {
	prepare(signer: auth(CreateAccount) &Account) {
		panic("Account initialized with custom script")
	}
}
