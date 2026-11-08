# 3-minute Loom: script and shot list

**Audience:** recruiters and engineers who will give you about 15 seconds before deciding to keep watching.
**Goal:** they remember one sentence: *"He built a Go engine that gives you the odds each car is right for you, and the AI can't make up a single number."*
**Shape:** hook → live product → three engineering proofs → close. About 400 spoken words.

---

## Before you record (15 minutes)

- [ ] `docker compose up -d --build`, seed, `cd web && npm run dev`. Click through the app once so everything is warm.
- [ ] Do one full interview in **Tab A** and stop on the results page (your safety net if something breaks live).
- [ ] Open a fresh session's shortlist page in **Tab B** and **Tab C** (same URL, for the conflict demo).
- [ ] A terminal at the repo root, font size 18+, dark theme, with these commands ready in its history:
  - `go test -race -run TestRun_IdenticalAcrossWorkerCounts -v ./internal/sim/`
  - `docker compose exec -T api /tiebreaker verify-run -run-id <a run id from Tab A>`
  - `go test -race -tags=integration -run TestWorkerCrash -v ./internal/jobs/`
  - `go test -run TestValidate_Table -v ./internal/report/`
- [ ] GitHub open on the Actions tab (all five jobs green) and on README → Benchmarks.
- [ ] Close Slack and notifications; browser zoom 110–125 %.
- [ ] If you've added real cars, use them. If not, say clearly that the cars are fictional sample data. Owning this out loud makes you more credible.

---

## Script

### 0:00–0:15 · Hook (face cam, no slides)
> "Comparison sites give you a spec table and leave the hard part to you. I built an engine that answers the actual question, *which of these cars is right for me?*, as a probability. Fifty thousand simulated futures in thirty-three milliseconds. And the AI in it isn't allowed to make up a single number."

**On screen:** cut straight to the results page in Tab A, with the winner and its percentage visible.

### 0:15–0:30 · The problem, in one breath
> "When you're choosing between two or three cars, you're really weighing fuel prices you can't predict, how much you'll drive, resale, repairs, and things like safety that you value but can't price. Tiebreaker puts all of that into rupees and simulates it."

**On screen:** the landing page; type a state; *Start comparing*.

### 0:30–1:30 · Live demo (screen only)
1. **Shortlist (0:30):** search, add three cars. Point at the **unverified data** badge: *"Every value needs a source and a date; unverified data is labelled, never hidden."* Set a budget and home charging → Continue.
2. **Interview (0:45):** answer the usage questions quickly. On the first ₹ question:
   > "Here's the interesting bit. It doesn't ask a fixed questionnaire. For every possible question it runs quick simulations and asks the one that would change the answer most: information gain. And it prices your answer in rupees: would you pay a lakh more for this safety rating?"

   Point at the **"Right now"** bars moving after each answer. Stop when it says it has enough.
3. **Simulation (1:10):** click *Run the full simulation*. The progress bar fills over server-sent events.
4. **Results (1:15):** winner and probability, the histogram, pros and cons *in rupees*, and the tipping point:
   > "If no car wins clearly, it tells you the one thing that would decide it." (Read out the tipping-point sentence on your screen, e.g. "above about 1,500 km a month, this one wins".)

### 1:30–2:35 · Three engineering proofs (terminal and code)
Say one sentence, show the evidence, move on.

1. **Deterministic concurrency (1:30):** run the identical-across-workers test, then `verify-run`.
   > "It's parallel across all cores, but the results are bit-for-bit identical on one, four or sixteen workers: each chunk gets its own seeded random stream. Every share link can be re-run to the exact same bits: here's a stored run reproducing its hash."
2. **Correct under real-world chaos (1:55):** in Tabs B and C, add a car in B, then add one in C. C shows *"changed in another tab"*. Then run the worker-crash test.
   > "Two tabs, one session: exactly one write wins, with optimistic locking and ETags, and retries are idempotent. Jobs run on a Postgres SKIP LOCKED queue with leases. Kill a worker mid-job and another one finishes it, and the dead one can't overwrite the result."
3. **The AI can't lie (2:15):** run the validator test, and show one rejected case on screen (e.g. "₹1.5 lakh" for a ₹1,02,340 fact).
   > "The engine decides; Gemini only writes prose. Every number it writes is checked against the computed facts, with rounding allowed. One invented number and its text is thrown away."

### 2:35–2:50 · Discipline (GitHub)
Scroll the green Actions run (lint, unit, race, integration, web), then the benchmark table.
> "Go, Postgres, Redis and Next.js, tests written first, race detector everywhere, real Postgres in CI, and every design decision written down with its reason."

### 2:50–3:00 · Close (face cam)
> "So: probabilities instead of spec tables, rupees instead of vibes, and an AI that has to show its work. The code's linked below. I'd love to talk about how it's built."

---

## Tips that make it land
- **Lead with the result, not the setup.** The hook already shows the answer screen; the demo then shows how you got there.
- **One idea per sentence, one proof per idea.** If you're explaining for more than 15 seconds without anything changing on screen, cut it.
- **Say numbers they'll remember:** 50,000 futures, 33 ms, 1/4/16 workers, 70 ms per question, 5 green CI jobs.
- **Do two takes:** one for the face-cam hook and close, one for the screen. Loom lets you trim; aim for 2:50 so you don't get cut off.
- **Title:** "Which car is right for *you*? A Go Monte Carlo engine with an AI that can't lie (3 min)."
- **Description:** the GitHub link, the live URL, and one line: "Go · Postgres · Redis · Next.js · Gemini: deterministic simulation, SKIP LOCKED job queue, fact-validated LLM."
