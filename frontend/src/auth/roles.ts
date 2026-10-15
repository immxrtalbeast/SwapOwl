// Роли и права (спецификация, 2.9): viewer < operator < admin.
export type Role = "viewer" | "operator" | "admin";

const rank: Record<Role, number> = { viewer: 0, operator: 1, admin: 2 };

export const roleLabel: Record<Role, string> = {
  viewer: "просмотр",
  operator: "оператор",
  admin: "администратор",
};

export function hasRole(role: Role | null | undefined, min: Role): boolean {
  return role != null && rank[role] >= rank[min];
}
