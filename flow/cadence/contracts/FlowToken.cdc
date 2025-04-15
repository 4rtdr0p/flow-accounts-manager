import FungibleToken from "./FungibleToken.cdc"
import ViewResolver from "./ViewResolver.cdc"
import Burner from "./Burner.cdc"

access(all) contract FlowToken: FungibleToken {

    // Total supply of Flow tokens in existence
    access(all) var totalSupply: UFix64

    // Event that is emitted when the contract is created
    access(all) event TokensInitialized(initialSupply: UFix64)

    // Event that is emitted when tokens are withdrawn from a Vault
    access(all) event TokensWithdrawn(amount: UFix64, from: Address?, uuid: UInt64, providerUUID: UInt64)

    // Event that is emitted when tokens are deposited to a Vault
    access(all) event TokensDeposited(amount: UFix64, to: Address?, uuid: UInt64, receiverUUID: UInt64)

    // Event that is emitted when tokens are burned
    access(all) event TokensBurned(amount: UFix64, fromUUID: UInt64)

    // Storage and Public Paths
    access(all) let VaultStoragePath: StoragePath
    access(all) let VaultPublicPath: PublicPath
    access(all) let ReceiverPublicPath: PublicPath
    access(all) let AdminStoragePath: StoragePath

    // Entitlements
    access(all) entitlement Withdraw

    // The implementation of the Vault resource that holds the tokens that are owned by an account
    access(all) resource Vault: FungibleToken.Vault, ViewResolver.Resolver {
        // The total balance of this vault
        access(all) var balance: UFix64

        // Initialize the balance at resource creation time
        init(balance: UFix64) {
            self.balance = balance
        }

        // Function that takes an amount as an argument
        // and withdraws that amount from the Vault
        access(Withdraw) fun withdraw(amount: UFix64): @{FungibleToken.Vault} {
            self.withdrawn(amount: amount)
            self.balance = self.balance - amount
            return <-create Vault(balance: amount)
        }

        // Function that deposits tokens into the vault
        access(all) fun deposit(from: @{FungibleToken.Vault}) {
            let vault <- from as! @FlowToken.Vault
            self.deposited(amount: vault.balance)
            self.balance = self.balance + vault.balance
            destroy vault
        }

        // Called when `withdraw` is called
        access(all) fun withdrawn(amount: UFix64) {
            pre {
                self.balance >= amount:
                    "Amount withdrawn must be less than or equal to the balance of the Vault"
            }
            post {
                self.balance == before(self.balance) - amount:
                    "Incorrect amount withdrawn"
                emit TokensWithdrawn(amount: amount, from: self.owner?.address, uuid: before(self).uuid, providerUUID: before(self).uuid)
            }
        }

        // Called when `deposit` is called
        access(all) fun deposited(amount: UFix64) {
            post {
                self.balance == before(self.balance) + amount:
                    "Incorrect amount deposited"
                emit TokensDeposited(amount: amount, to: self.owner?.address, uuid: before(self).uuid, receiverUUID: before(self).uuid)
            }
        }
        
        // Called when this vault is destroyed with Burner.burn()
        access(contract) fun burnCallback() {
            pre {
                emit TokensBurned(amount: self.balance, fromUUID: self.uuid)
            }
            post {
                self.balance == 0.0:
                    "Flow token vault balance must be set to zero when burned"
            }
            FlowToken.totalSupply = FlowToken.totalSupply - self.balance
            self.balance = 0.0
        }
        
        // Implement ViewResolver.Resolver interface
        access(all) view fun getViews(): [Type] {
            return []
        }
        
        access(all) view fun resolveView(_ view: Type): AnyStruct? {
            return nil
        }
        
        // These functions implement the FungibleToken interface
        access(all) view fun getSupportedVaultTypes(): [Type] {
            return [Type<@FlowToken.Vault>()]
        }
        
        access(all) view fun isAvailableToWithdraw(amount: UFix64): Bool {
            return self.balance >= amount
        }
        
        access(all) view fun isAvailableToDeposit(from: &{FungibleToken.Vault}): Bool {
            return from.getType() == Type<@FlowToken.Vault>()
        }
    }

    // Function that creates a new token Vault with an initial balance
    // and returns it to the calling context
    access(all) fun createEmptyVault(): @{FungibleToken.Vault} {
        return <-create Vault(balance: 0.0)
    }

    access(all) resource Administrator {
        // Create a new minter resource
        // this can be used by an admin to create more tokens,
        // for example if the total supply needs to increase
        access(all) fun createNewMinter(allowedAmount: UFix64): @Minter {
            return <-create Minter(allowedAmount: allowedAmount)
        }
    }

    // Resource that allows an admin to mint new tokens
    access(all) resource Minter {
        // The amount of tokens that the minter is allowed to mint
        access(all) var allowedAmount: UFix64

        // Initialize the allowed amount
        init(allowedAmount: UFix64) {
            self.allowedAmount = allowedAmount
        }

        // Mint new tokens, adds them to the total supply,
        // and returns them to the caller
        access(all) fun mintTokens(amount: UFix64): @{FungibleToken.Vault} {
            pre {
                amount > 0.0:
                    "Amount minted must be greater than zero"
                amount <= self.allowedAmount:
                    "Amount minted must be less than or equal to the allowed amount"
            }
            post {
                self.allowedAmount == before(self.allowedAmount) - amount:
                    "Minter allowedAmount must be decreased by the amount minted"
            }
            self.allowedAmount = self.allowedAmount - amount
            FlowToken.totalSupply = FlowToken.totalSupply + amount
            return <-create Vault(balance: amount)
        }
    }

    init() {
        self.totalSupply = 0.0

        self.VaultStoragePath = /storage/flowTokenVault
        self.VaultPublicPath = /public/flowTokenVault
        self.ReceiverPublicPath = /public/flowTokenReceiver
        self.AdminStoragePath = /storage/flowTokenAdmin

        // Create the Vault with the total supply of tokens and save it in storage
        let vault <- create Vault(balance: self.totalSupply)
        self.account.storage.save(<-vault, to: self.VaultStoragePath)

        // Create a public capability to the stored Vault that only exposes
        // the balance field through the Balance interface
        let cap = self.account.capabilities.storage.issue<&{FungibleToken.Balance}>(
            self.VaultStoragePath
        )
        self.account.capabilities.publish(cap, at: self.VaultPublicPath)

        // Create a public capability to the stored Vault that only exposes
        // the deposit function through the Receiver interface
        let receiverCap = self.account.capabilities.storage.issue<&{FungibleToken.Receiver}>(
            self.VaultStoragePath
        )
        self.account.capabilities.publish(receiverCap, at: self.ReceiverPublicPath)

        // Create an admin resource and save it to storage
        let admin <- create Administrator()
        self.account.storage.save(<-admin, to: self.AdminStoragePath)

        emit TokensInitialized(initialSupply: self.totalSupply)
    }
}
