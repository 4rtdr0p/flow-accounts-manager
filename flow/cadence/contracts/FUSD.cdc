import FungibleToken from "./FungibleToken.cdc"
import ViewResolver from "./ViewResolver.cdc"

access(all) contract FUSD: FungibleToken {

    access(all) var totalSupply: UFix64

    access(all) event TokensInitialized(initialSupply: UFix64)
    access(all) event TokensMinted(amount: UFix64)

    access(all) let VaultStoragePath: StoragePath
    access(all) let VaultPublicPath: PublicPath
    access(all) let ReceiverPublicPath: PublicPath
    access(all) let AdminStoragePath: StoragePath

    access(all) view fun getContractViews(resourceType: Type?): [Type] {
        return []
    }

    access(all) fun resolveContractView(resourceType: Type?, viewType: Type): AnyStruct? {
        return nil
    }

    access(all) resource Vault: FungibleToken.Vault {

        access(all) var balance: UFix64

        init(balance: UFix64) {
            self.balance = balance
        }

        access(contract) fun burnCallback() {
            if self.balance > 0.0 {
                FUSD.totalSupply = FUSD.totalSupply - self.balance
            }
            self.balance = 0.0
        }

        access(all) view fun getViews(): [Type] {
            return FUSD.getContractViews(resourceType: nil)
        }

        access(all) fun resolveView(_ view: Type): AnyStruct? {
            return FUSD.resolveContractView(resourceType: nil, viewType: view)
        }

        access(all) view fun isAvailableToWithdraw(amount: UFix64): Bool {
            return amount <= self.balance
        }

        access(FungibleToken.Withdraw) fun withdraw(amount: UFix64): @FUSD.Vault {
            self.balance = self.balance - amount
            return <-create Vault(balance: amount)
        }

        access(all) fun deposit(from: @{FungibleToken.Vault}) {
            let vault <- from as! @FUSD.Vault
            self.balance = self.balance + vault.balance
            destroy vault
        }

        access(all) fun createEmptyVault(): @FUSD.Vault {
            return <-create Vault(balance: 0.0)
        }
    }

    access(all) resource Minter {
        access(all) fun mintTokens(amount: UFix64): @FUSD.Vault {
            FUSD.totalSupply = FUSD.totalSupply + amount
            emit TokensMinted(amount: amount)
            return <-create Vault(balance: amount)
        }
    }

    access(all) fun createEmptyVault(vaultType: Type): @FUSD.Vault {
        return <-create Vault(balance: 0.0)
    }

    init() {
        self.totalSupply = 0.0

        self.VaultStoragePath = /storage/fusdVault
        self.VaultPublicPath = /public/fusdBalance
        self.ReceiverPublicPath = /public/fusdReceiver
        self.AdminStoragePath = /storage/fusdAdmin

        let minter <- create Minter()
        let vault <- minter.mintTokens(amount: 1000000.0)
        destroy minter

        self.account.storage.save(<-vault, to: self.VaultStoragePath)

        let balanceCap = self.account.capabilities.storage.issue<&FUSD.Vault>(self.VaultStoragePath)
        self.account.capabilities.publish(balanceCap, at: self.VaultPublicPath)

        let receiverCap = self.account.capabilities.storage.issue<&FUSD.Vault>(self.VaultStoragePath)
        self.account.capabilities.publish(receiverCap, at: self.ReceiverPublicPath)

        emit TokensInitialized(initialSupply: self.totalSupply)
    }
}
