# Digital Fixed Deposits, Explained Simply

## What was built

A working demonstration of a **Fixed Deposit (FD)** — the kind of savings
product where you lock money away for a set period and earn interest — managed
entirely on a **blockchain** instead of a bank's traditional internal database.

This is the first working example built on **Drunix**, a blockchain platform
developed by NPCI (the organization behind India's UPI payment system). Drunix
is designed for banks and financial institutions to jointly run shared,
tamper-resistant record books — no single bank controls the ledger alone.

## Why this matters, in plain terms

Normally, if you open a Fixed Deposit, your bank keeps a record in its own
private database. You have to trust that one bank to keep that record honest
and unchanged. With a blockchain-based FD, the record instead lives on a
**shared ledger** that multiple organizations hold identical copies of — think
of it like a shared logbook where every entry needs to be independently
double-checked and stamped by more than one party before it counts. No single
party can quietly alter it.

This demo builds and proves out the basic rules a real FD needs, running for
real on such a shared ledger:

- **Identity checks (KYC)** — recording who the depositor is before anything else happens.
- **Locking the money** — the deposit genuinely cannot be withdrawn before its maturity date; the system rejects early withdrawal attempts automatically.
- **Regulatory recall (clawback)** — if a deposit needs to be frozen for review (for example, an anti-money-laundering flag), an authorized bank can freeze it, and later release it once cleared.
- **Maturity payout** — once the deposit's term is up, it can be redeemed for the original amount plus interest, and the record is marked as completed.

## What was actually proven, not just designed

This isn't a slideshow or a mockup — it was run on a genuinely live,
two-organization blockchain network, with every step checked directly against
the ledger's own recorded data, not just taken on faith from a "success"
message. Specifically, all of the following were demonstrated and independently
verified:

1. A depositor's identity was registered.
2. A deposit was created (minted) with a real principal amount and interest rate.
3. An attempt to withdraw the money early was correctly and automatically blocked.
4. A deposit was frozen (clawed back) for review, then released again once cleared.
5. After the deposit's term ended, it was successfully redeemed for the full amount plus interest.

## What this is not (yet)

To be clear about the current scope: this is a **working proof of concept**,
not a finished product ready for real customers. In this demonstration, one
identity played both the "bank" role and the "depositor" role for simplicity —
a real deployment would use genuinely separate accounts for the bank and each
individual customer. No performance testing, security auditing, or real
customer data was involved.

## Why "first use case" matters

This is the first time a complete, working financial product lifecycle has
been built and verified end-to-end on the Drunix platform. It serves as a
template and proof point for what future blockchain-based banking products on
this platform could look like — showing that the core building blocks
(identity checks, time-based locks, regulatory controls, and payouts) can all
be enforced reliably by the platform itself, rather than relying on a single
party's word.
