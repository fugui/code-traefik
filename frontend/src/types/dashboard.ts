export type HealthGrade = 'EXCELLENT' | 'GOOD' | 'SUBHEALTH' | 'CRITICAL';

export interface ViolatedProcessStat {
  arch_process_id: number;
  arch_process_code: string;
  arch_process_name: string;
  violation_count: number;
  subsystem_id?: number;
  subsystem_name?: string;
}

export interface OrphanServiceSummary {
  service_name: string;
  schema_id: number;
  schema_name: string;
  operation_count: number;
}

export interface HealthScoreDTO {
  total: number;
  grade: HealthGrade;
  arch_sub_score: number;
  asset_sub_score: number;
  regression_sub_score: number;
  action_hints: string[];
}

export interface DriftRadarDTO {
  verified_edges: number;
  active_violation_edges: number;
  top_violated_processes: ViolatedProcessStat[];
}

export interface ContractAssetsDTO {
  total_active_services: number;
  claimed_services: number;
  orphan_services: number;
  claimed_ratio: number;
  multi_impl_contract_count?: number;
  multi_impl_service_count?: number;
  top_orphan_services: OrphanServiceSummary[];
}

export interface RegressionDefenseDTO {
  gate_evaluations_30d: number;
  blocked_count_30d: number;
  pass_rate_30d: number;
  unhealthy_suite_processes_7d: number;
}

export interface DashboardOverviewDTO {
  health_score: HealthScoreDTO;
  drift_radar: DriftRadarDTO;
  contract_assets: ContractAssetsDTO;
  regression_defense: RegressionDefenseDTO;
}
