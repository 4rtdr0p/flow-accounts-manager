transaction(newPublicKeyHex: String, oldKeyIndex: Int) {
  prepare(signer: auth(Keys) &Account) {
    let publicKey = PublicKey(
      publicKey: newPublicKeyHex.decodeHex(),
      signatureAlgorithm: SignatureAlgorithm.ECDSA_P256
    )

    signer.keys.add(
      publicKey: publicKey,
      hashAlgorithm: HashAlgorithm.SHA3_256,
      weight: 1000.0
    )

    signer.keys.revoke(keyIndex: oldKeyIndex)
  }
}
