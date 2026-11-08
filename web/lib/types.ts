// Types mirroring the Go API's JSON (internal/session, internal/report, …).

export type SessionState = "SHORTLISTING" | "USAGE" | "INTERVIEW" | "SIMULATING" | "DONE";

export interface Variant {
  id: string;
  display_name: string;
  fuel_type: string;
  body_type: string;
  transmission: string;
  seats: number;
  ex_showroom_price_inr: number | null;
  launch_year: number;
  discontinued_year: number | null;
  safety_rating_stars: number | null;
  verified: boolean;
  warnings: string[];
}

export interface Elimination {
  constraint: string;
  required: string;
  actual: string;
  shortfall: number;
  message: string;
}

export interface CarView {
  id: string;
  variant_id: string;
  condition: "new" | "used";
  label: string;
  fuel_type: string;
  verified: boolean;
  warnings: string[];
  eliminated: boolean;
  eliminations?: Elimination[];
  price?: { upfront_inr: number; budget_inr: number; source?: "estimate" | "catalogue" | "override" | "asking"; insurance_included?: boolean };
}

export interface SessionView {
  id: string;
  version: number;
  state: SessionState;
  user_state: string;
  cars: CarView[];
  constraints: Record<string, unknown> | null;
  usage_complete: boolean;
  questions_asked: number;
}

export interface Question {
  question_id: string;
  phase: "usage" | "tradeoff";
  kind: "range" | "number" | "financing" | "choice" | "yes_no";
  text: string;
  unit?: string;
  min?: number;
  max?: number;
  options?: string[];
  optional?: boolean;
  example?: string;
  attribute?: string;
  threshold_inr?: number;
  warning?: string;
}

export interface NextResponse {
  done: boolean;
  reason?: string;
  question?: Question;
  p_best?: { item_id: string; label: string; p_best: number }[];
  questions_asked: number;
  questions_left: number;
  fell_back?: boolean;
}

export interface ProgressEvent {
  run_id: string;
  status: "queued" | "running" | "done" | "failed" | "cancelled";
  progress: number;
  error?: string;
}

export interface Percentiles {
  p10: number;
  p50: number;
  p90: number;
}

export interface Point {
  source: "preference" | "cost";
  key: string;
  text: string;
  value_inr: number;
}

export interface CarFacts {
  item_id: string;
  label: string;
  verified: boolean;
  safety_rating: string;
  p_best: number;
  p_cheapest: number;
  net_cost: Percentiles;
  ownership_cost: Percentiles;
  breakdown: { category: string; median_inr: number }[];
  pros: Point[];
  cons: Point[];
  not_asked: string[];
  tipping_points: string[];
  warnings: string[];
}

export interface Facts {
  kind: "winner" | "deadlock" | "none_fit";
  deadlock_type?: string;
  winner?: string;
  horizon_years?: number;
  run?: { id: string; model_version: string; seed: string; futures: number };
  cars: CarFacts[];
  tipping_points: { sentence: string; inside_user_range: boolean }[];
  eliminated: { label: string; reasons: string[] }[];
  warnings: string[];
  placeholder_assumptions: number;
}

export interface ResultRow {
  shortlist_item_id: string;
  p_best: number;
  histogram: { edges_inr: number[]; counts: number[] };
}

export interface ReportView {
  kind: string;
  share_id: string;
  share_path: string;
  run_id?: string;
  facts: Facts;
  prose_md: string;
  prose_source: "template" | "llm";
  results?: ResultRow[];
}
