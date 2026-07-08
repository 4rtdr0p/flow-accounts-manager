transaction(newPublicKeyHex: String, oldKeyIndex: Int, signAlgo: String, hashAlgo: String, weight: UFix64) {
  prepare(signer: auth(Keys) &Account) {
    var signatureAlgorithm = SignatureAlgorithm.ECDSA_P256
    if signAlgo == "ECDSA_secp256k1" {
      signatureAlgorithm = SignatureAlgorithm.ECDSA_secp256k1
    } else if signAlgo != "ECDSA_P256" {
      panic("unsupported signature algorithm")
    }

    var hashAlgorithm = HashAlgorithm.SHA3_256
    if hashAlgo == "SHA2_256" {
      hashAlgorithm = HashAlgorithm.SHA2_256
    } else if hashAlgo != "SHA3_256" {
      panic("unsupported hash algorithm")
    }

    let publicKey = PublicKey(
      publicKey: newPublicKeyHex.decodeHex(),
      signatureAlgorithm: signatureAlgorithm
    )

    signer.keys.add(
      publicKey: publicKey,
      hashAlgorithm: hashAlgorithm,
      weight: weight
    )

    signer.keys.revoke(keyIndex: oldKeyIndex)
  }
}
