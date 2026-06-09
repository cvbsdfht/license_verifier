package license

import "github.com/gofiber/fiber/v3"

func (r *license) FiberMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if r.isServing() {
			return c.Next()
		}

		st := r.getState()
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "LICENSE_INVALID",
			"status":  st.Status,
			"reason":  st.Reason,
			"message": "Maintenance agreement (MA) has ended or the license is invalid. Please contact your vendor to renew.",
		})
	}
}

// exposes the license status (no gate) for the frontend to display
func (r *license) StatusHandler() fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.JSON(r.getState())
	}
}
