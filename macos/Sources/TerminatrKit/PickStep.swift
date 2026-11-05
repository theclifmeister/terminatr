/// Where ↑ and ↓ go in a pane's list to pick from (T1002): the next id,
/// the first with none picked, nothing past either end or for another key.
public enum PickStep {
    public static func next<ID: Hashable>(_ order: [ID], from: ID?, up: Bool, down: Bool) -> ID? {
        guard up || down, !order.isEmpty else { return nil }
        guard let from, let i = order.firstIndex(of: from) else { return order.first }
        let j = up ? i - 1 : i + 1
        return order.indices.contains(j) ? order[j] : nil
    }
}
