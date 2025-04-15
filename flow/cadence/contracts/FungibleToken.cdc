/**

# The Flow Fungible Token standard

## `FungibleToken` contract interface

The interface that all fungible token contracts would have to conform to.
If a users want to deploy a new token contract, their contract
would need to implement the FungibleToken interface.

Their contract would have to follow all the rules and naming
that the interface specifies.

## `Vault` resource

The resource that holds the balance of a users tokens.

## `Provider`, `Receiver`, and `Balance` resource interfaces

These interfaces declare functions with specific requirements that
the implementer can use to communicate with other token objects

## `TokensInitialized`, `TokensWithdrawn`, and `TokensDeposited` events

Events that are emitted when a token contract is created
and when tokens are moved between accounts.

Repo reference: https://github.com/onflow/flow-ft
*/

import "ViewResolver"

/// FungibleToken
///
/// The interface that fungible token contracts should implement.
///
access(all) contract interface FungibleToken: ViewResolver {

    /// An entitlement for allowing the withdrawal of tokens from a Vault
    access(all) entitlement Withdraw

    /// An entitlement for allowing the deposit of tokens into a Vault
    access(all) entitlement Deposit

    /// The total number of tokens in existence.
    /// It is up to the implementer to ensure that the total supply
    /// stays accurate and up to date
    ///
    access(all) var totalSupply: UFix64

    /// TokensInitialized
    ///
    /// The event that is emitted when the contract is created
    ///
    access(all) event TokensInitialized(initialSupply: UFix64)

    /// TokensWithdrawn
    ///
    /// The event that is emitted when tokens are withdrawn from a Vault
    ///
    access(all) event TokensWithdrawn(amount: UFix64, from: Address?, uuid: UInt64, providerUUID: UInt64)

    /// TokensDeposited
    ///
    /// The event that is emitted when tokens are deposited to a Vault
    ///
    access(all) event TokensDeposited(amount: UFix64, to: Address?, uuid: UInt64, receiverUUID: UInt64)

    /// Provider
    ///
    /// The interface that enforces the requirements for withdrawing
    /// tokens from the implementing type.
    ///
    /// It does not enforce requirements on `balance` here,
    /// because it leaves open the possibility of creating custom providers
    /// that do not necessarily need their own balance.
    ///
    access(all) resource interface Provider {
        /// withdraw
        ///
        /// The main function of the Provider interface, withdraw, allows
        /// anyone with a reference to this resource, given valid authorization,
        /// to withdraw tokens from it.
        ///
        /// The Provider must verify that the Vault's balance is at least
        /// the withdrawal amount, or else the Vault would have a negative balance.
        ///
        /// Parameters:
        ///   - amount: The amount of tokens to withdraw
        ///
        /// Returns: A Vault containing the withdrawn tokens.
        ///
        access(Withdraw) fun withdraw(amount: UFix64): @{FungibleToken.Vault} {
            post {
                result.balance == amount:
                    "FungibleToken.Provider.withdraw: Incorrect amount returned. Expected: ".concat(amount.toString()).concat(", Actual: ").concat(result.balance.toString())
                emit TokensWithdrawn(amount: amount, from: self.owner?.address, uuid: result.uuid, providerUUID: self.uuid)
            }
        }
        
        /// getSupportedVaultTypes
        ///
        /// Returns an array of Types that the implementing type supports for withdrawal.
        ///
        /// Returns: An array of Types.
        ///
        access(all) view fun getSupportedVaultTypes(): [Type] {
            return []
        }
        
        /// isAvailableToWithdraw
        ///
        /// Checks if a given amount is available to withdraw from the Vault.
        ///
        /// Parameters:
        ///   - amount: The amount to withdraw
        ///
        /// Returns: true if the requested amount is available to withdraw, false otherwise.
        ///
        access(all) view fun isAvailableToWithdraw(amount: UFix64): Bool {
            return false
        }
    }

    /// Receiver
    ///
    /// The interface that enforces the requirements for depositing
    /// tokens into the implementing type.
    ///
    /// We do not include a condition that checks the balance because
    /// we want to give users the ability to make custom receivers that
    /// can do custom things with the tokens, like split them up and
    /// send them to different places.
    ///
    access(all) resource interface Receiver {
        /// deposit
        ///
        /// The main function of the Receiver interface, deposit, allows
        /// anyone with a reference to this resource to deposit tokens into it.
        ///
        /// Parameters:
        ///   - from: A Vault containing the tokens to deposit
        ///
        access(all) fun deposit(from: @{FungibleToken.Vault}) {
            pre {
                from.balance > 0.0:
                    "FungibleToken.Receiver.deposit: Cannot deposit a Vault with zero balance."
                from.getType() != self.getType():
                    "FungibleToken.Receiver.deposit: Cannot deposit a Vault of the same type as the receiver."
            }
            post {
                emit TokensDeposited(amount: from.balance, to: self.owner?.address, uuid: from.uuid, receiverUUID: self.uuid)
            }
        }

        /// getSupportedVaultTypes
        ///
        /// Returns an array of Types that the implementing type supports for deposit.
        ///
        /// Returns: An array of Types.
        ///
        access(all) view fun getSupportedVaultTypes(): [Type] {
            return []
        }

        /// isAvailableToDeposit
        ///
        /// Checks if a given Vault is available to deposit into the Vault.
        ///
        /// Parameters:
        ///   - from: The vault to check
        ///
        /// Returns: true if the vault is accepted for deposit, false otherwise.
        ///
        access(all) view fun isAvailableToDeposit(from: &{FungibleToken.Vault}): Bool {
            return false
        }
    }

    /// Balance
    ///
    /// The interface that contains the `balance` field of the Vault
    /// and enforces that when new Vaults are created, the balance
    /// is initialized correctly.
    ///
    access(all) resource interface Balance {
        /// The total balance of a vault
        ///
        access(all) var balance: UFix64
    }

    /// Vault
    ///
    /// The resource that contains the functions to send and receive tokens.
    ///
    access(all) resource interface Vault: Provider, Receiver, Balance, ViewResolver.Resolver {
        /// balance of the vault
        access(all) var balance: UFix64

        /// withdrawn
        ///
        /// The Event that is emitted when tokens are withdrawn from the vault
        ///
        access(all) event TokenResourceDestroyed(uuid: UInt64 = self.uuid, balance: UFix64 = self.balance)

        /// createEmptyVault
        ///
        /// Function that creates a new Vault with a zero balance.
        ///
        /// Returns: A new Vault resource with a balance of zero.
        ///
        access(all) fun createEmptyVault(): @{FungibleToken.Vault} {
            post {
                result.balance == 0.0:
                    "FungibleToken.Vault.createEmptyVault: Balance must be zero when creating a new Vault!"
                result.getType() == self.getType():
                    "FungibleToken.Vault.createEmptyVault: Type of created vault must be the same as the creating vault!"
            }
        }
    }

    /// createEmptyVault
    ///
    /// Function that creates a new Vault with a zero balance that is used
    /// by the contract that implements the Vault interface.
    ///
    /// Returns: A new Vault resource with a balance of zero
    ///
    access(all) fun createEmptyVault(): @{FungibleToken.Vault} {
        post {
            result.balance == 0.0:
                "FungibleToken.createEmptyVault: Balance must be zero when creating a new Vault!"
        }
    }
}
