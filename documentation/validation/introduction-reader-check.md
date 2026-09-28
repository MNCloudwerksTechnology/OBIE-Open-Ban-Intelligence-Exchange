# Reader check: "What is OBIE?"

[What is OBIE?](../introduction.md) is written for decision makers without
a networking background. This page is how we check that such a reader can
follow it, and the record of every check. Repeat the check when the
introduction changes substantially.

Only a real person counts. A reader simulated by an AI model is useful for
a first draft, but it is not a reader check.

## Who can be the reader

- Someone who decides about IT, or pays for it, but does not run servers
  or networks: a managing director, an office manager, a buyer.
- They have never seen OBIE and did not help write the introduction.
- One reader is the minimum. A second reader usually finds new stumbling
  points.

## How to run it

It takes about 15 minutes. You, the moderator, stay silent while the
reader reads and do not explain anything.

1. Give the reader the introduction, rendered with its diagram: the page on
   GitHub or a printout. Following its links is allowed; note which ones
   they open.
2. Ask them to read it at their own pace and to mark every word or
   sentence they do not understand. Time the reading. Stop at 10 minutes.
3. Take the page away. Ask the questions below and write down the answers
   in the reader's own words. Do not correct or hint.
4. Ask which passage was hardest, and what they would still want to know.

## Questions and expected answers

An answer counts as correct if it has the meaning below, in any words.

| # | Question | Correct if it says |
|---|---|---|
| Q1 | What does OBIE do? | Servers warn each other about attackers, and each server still decides for itself what it blocks. |
| Q2 | Who decides whether an address is blocked on your server? | Your own server (your node), based on how much you trust each peer. A warning from another server is only advice. |
| Q3 | Why can OBIE not lock you out of your own server? | Your server's own addresses and the protected addresses are never blocked, and addresses on your allow-list are not blocked, whoever reports them. |
| Q4 | What does "observe mode" mean? | OBIE only lists what it would block and blocks nothing. It is how a new node starts, until you switch to enforce mode. |
| Q5 | What leaves your server, and what never does? | Signed warnings about attacking addresses (which address, on which service, when) go to your peers. The raw logs, user names and passwords never leave. |
| Q6 | Is OBIE relevant for you, and why? | Any reasoned answer. It shows whether the page lets the reader decide. |

Q1, Q3 and Q4 are the three things every newcomer must be able to explain
after reading, the success criteria of the first-time user experience
(WP-1681).

## When the check is passed

- The reader answers Q1 to Q5 correctly after at most 10 minutes of
  reading.
- Every passage the reader marked or stumbled over is rewritten, and
  `TestIntroductionIsPlainLanguage` still passes. If a question was
  answered wrongly, show the reader the rewritten passage and ask again.

## Results

Record each check here: the reader's role, never their name.

| Date | Reader (role, background) | Reading time | Q1 to Q5 correct | Stumbling points | Changes made |
|---|---|---|---|---|---|
| pending | No real reader has checked the introduction yet. | | | | |

On 2026-09-28 an AI model played a decision maker without IT background
(WP-1691). Its findings led to clearer wording: "internet addresses (IP
addresses)", protected addresses and the allow-list, observe mode, how a
wrongly blocked customer is unblocked, data protection, and why a single
server without peers gains little. It does not replace the check above.
